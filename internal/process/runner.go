// Package process runs commands owned by Sheldon without invoking a shell.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ErrOutputLimit indicates that stdout or stderr exceeded its configured bound.
var ErrOutputLimit = errors.New("process output limit exceeded")

// Command describes one executable invocation. Env overrides inherited values;
// it is never logged. OutputLimit bounds each output stream in bytes (default
// 4 MiB). GracePeriod bounds graceful shutdown before forced cleanup (default
// two seconds). Dir must be absolute. Arguments are passed literally.
type Command struct {
	Name        string
	Args        []string
	Dir         string
	Env         []string
	OutputLimit int
	GracePeriod time.Duration
}

// Result contains the process exit code and captured output. Stdout is populated
// by Run; Start exposes its output through Process.Stdout instead.
type Result struct {
	Stdout   []byte `json:"-"`
	ExitCode int    `json:"exitCode"`
	stderr   []byte
}

// Stderr returns bounded, untrusted diagnostic bytes for backend inspection.
// Callers must redact before displaying them; errors and normal formatting
// deliberately withhold both output streams rather than guessing at secrets.
func (r Result) Stderr() []byte {
	return bytes.Clone(r.stderr)
}

// String returns the process exit code without exposing captured output.
func (r Result) String() string {
	return fmt.Sprintf("process exit code %d (output withheld)", r.ExitCode)
}

// GoString returns the same output-safe representation for fmt's %#v format.
func (r Result) GoString() string {
	return r.String()
}

// Process owns one child and its pipes. Consume Stdout with a single reader;
// call Close when finished, including after Wait, to release the reader.
type Process struct {
	Stdin     io.WriteCloser
	Stdout    io.ReadCloser
	cancel    context.CancelCauseFunc
	done      chan struct{}
	result    Result
	err       error
	closeOnce sync.Once
}

// Start spawns a command with streaming stdout, writable stdin, and separately
// bounded stderr. The context controls the entire process lifetime.
func Start(ctx context.Context, spec Command) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec.Name == "" || !filepath.IsAbs(spec.Dir) {
		return nil, errors.New("process requires an executable and absolute working directory")
	}
	if spec.OutputLimit < 0 || spec.GracePeriod < 0 {
		return nil, errors.New("process limits must not be negative")
	}
	if spec.OutputLimit == 0 {
		spec.OutputLimit = 4 << 20
	}
	if spec.GracePeriod == 0 {
		spec.GracePeriod = 2 * time.Second
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(cmd.Environ(), spec.Env...)
	cmd.WaitDelay = spec.GracePeriod
	// Own this pipe rather than StdoutPipe: cmd.Wait must not close the reader
	// before an adapter has consumed the final protocol bytes.
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel(err)
		return nil, err
	}
	cmd.Stdout = writer
	stderr := &capture{limit: spec.OutputLimit, cancel: cancel}
	cmd.Stderr = stderr
	tree, err := prepareTree(cmd)
	if err != nil {
		reader.Close()
		writer.Close()
		cancel(err)
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		tree.close()
		reader.Close()
		writer.Close()
		cancel(err)
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		tree.close()
		reader.Close()
		writer.Close()
		stdin.Close()
		cancel(err)
		return nil, fmt.Errorf("start process: %w", err)
	}
	writer.Close()
	if err = tree.started(); err != nil {
		tree.kill()
		cmd.Process.Kill()
		cmd.Wait()
		tree.close()
		reader.Close()
		stdin.Close()
		cancel(err)
		return nil, fmt.Errorf("own process tree: %w", err)
	}
	p := &Process{Stdin: stdin, Stdout: &outputReader{file: reader, remaining: spec.OutputLimit, cancel: cancel}, cancel: cancel, done: make(chan struct{})}
	go func() {
		wait := make(chan error, 1)
		go func() { wait <- cmd.Wait() }()
		var waitErr error
		select {
		case waitErr = <-wait:
		case <-runCtx.Done():
			stdin.Close()
			tree.terminate()
			timer := time.NewTimer(spec.GracePeriod)
			select {
			case waitErr = <-wait:
			case <-timer.C:
				tree.kill()
				waitErr = <-wait
			}
			timer.Stop()
		}
		// A child may leave workers behind even when it exits normally.
		cleanupErr := errors.Join(tree.kill(), tree.close())
		stdin.Close()
		p.result = Result{ExitCode: cmd.ProcessState.ExitCode(), stderr: bytes.Clone(stderr.Bytes())}
		if cause := context.Cause(runCtx); cause != nil {
			waitErr = cause
		}
		p.err = errors.Join(waitErr, cleanupErr)
		close(p.done)
		cancel(nil)
	}()
	return p, nil
}

// Run executes a finite command with closed stdin and bounded captured output.
func Run(ctx context.Context, spec Command) (Result, error) {
	p, err := Start(ctx, spec)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer p.Close()
	p.Stdin.Close()
	stdout, readErr := io.ReadAll(p.Stdout)
	if readErr != nil {
		p.cancel(readErr)
	}
	result, err := p.Wait()
	result.Stdout = stdout
	return result, errors.Join(err, readErr)
}

// Wait waits for process termination and owned-tree cleanup. It does not consume
// stdout; use Run for capture, or consume the stream before/concurrently with Wait.
func (p *Process) Wait() (Result, error) {
	<-p.done
	return p.result, p.err
}

// Close cancels the owned process, waits for cleanup and closes its pipes.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		p.cancel(context.Canceled)
		<-p.done
		p.Stdin.Close()
		p.Stdout.Close()
	})
	return nil
}

type capture struct {
	buffer bytes.Buffer
	limit  int
	cancel context.CancelCauseFunc
}

// Write captures data up to the byte limit and cancels the process on overflow.
// Excess data is discarded while the stream is drained.
func (b *capture) Write(data []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	b.buffer.Write(data[:min(len(data), remaining)])
	if len(data) > remaining {
		b.cancel(ErrOutputLimit)
	}
	return len(data), nil
}

// Bytes returns the captured output bytes.
func (b *capture) Bytes() []byte { return b.buffer.Bytes() }

type outputReader struct {
	file      *os.File
	remaining int
	cancel    context.CancelCauseFunc
}

// Read reads within the remaining byte limit, canceling the process and returning
// ErrOutputLimit when it detects excess output.
func (r *outputReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.file.Read(extra[:])
		if n > 0 {
			r.cancel(ErrOutputLimit)
			return 0, ErrOutputLimit
		}
		return 0, err
	}
	n, err := r.file.Read(data[:min(len(data), r.remaining)])
	r.remaining -= n
	return n, err
}

// Close closes the stdout pipe reader.
func (r *outputReader) Close() error { return r.file.Close() }

package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func helper(t *testing.T, mode string, args ...string) Command {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "working directory 日本語")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return Command{
		Name: os.Args[0], Args: append([]string{"-test.run=^TestHelperProcess$", "--", mode}, args...),
		Dir: dir, Env: []string{"SHELDON_PROCESS_TEST_HELPER=1"}, GracePeriod: 100 * time.Millisecond,
	}
}

func TestRunLiteralArgumentsAndEnvironment(t *testing.T) {
	t.Setenv("SHELDON_PROCESS_TEST_VALUE", "inherited")
	spec := helper(t, "echo", "has spaces", "日本語", "; touch should-not-exist", "$(touch should-not-exist)")
	spec.Env = append(spec.Env, "SHELDON_PROCESS_TEST_VALUE=overridden")
	r, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Dir   string
		Args  []string
		Value string
	}
	if err := json.Unmarshal(r.Stdout, &output); err != nil {
		t.Fatal(err)
	}
	if output.Dir != spec.Dir || output.Value != "overridden" || strings.Join(output.Args, "|") != strings.Join(spec.Args[3:], "|") {
		t.Fatalf("unexpected command result: %s", r.Stdout)
	}
	if r.ExitCode != 0 || string(r.Stderr()) != "separate diagnostic\n" {
		t.Fatal("output streams were combined or exit code missing")
	}
	if _, err := os.Stat(filepath.Join(spec.Dir, "should-not-exist")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("arguments were executed as shell code")
	}
}

func TestFailureDoesNotExposeDiagnostics(t *testing.T) {
	r, err := Run(context.Background(), helper(t, "fail"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || r.ExitCode != 7 {
		t.Fatalf("exit code=%d error=%v", r.ExitCode, err)
	}
	if string(r.Stderr()) != "provider-secret-do-not-log\n" {
		t.Fatal("stderr was not retained separately for explicit backend inspection")
	}
	for _, text := range []string{err.Error(), fmt.Sprint(r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r)} {
		if strings.Contains(text, "provider-secret") {
			t.Fatal("raw stderr leaked into error/result formatting")
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatal("raw output leaked into result JSON")
	}
}

func TestStartStreamsBeforeExitAndAcceptsInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := Start(ctx, helper(t, "stream"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := bufio.NewReader(p.Stdout)
	line, err := r.ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("readiness=%q error=%v", line, err)
	}
	select {
	case <-p.done:
		t.Fatal("Start/readiness was incorrectly treated as completion")
	default:
	}
	if _, err := io.WriteString(p.Stdin, "hello\n"); err != nil {
		t.Fatal(err)
	}
	p.Stdin.Close()
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "reply:hello\n" {
		t.Fatalf("lost fragmented/final output: %q", output)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestFinalStdoutRemainsReadableAfterWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := Start(ctx, helper(t, "output", "stdout", "16"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(p.Stdout)
	if err != nil || string(b) != strings.Repeat("x", 16) {
		t.Fatalf("Wait closed unread stdout: bytes=%d error=%v", len(b), err)
	}
}

func TestOutputBounds(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		for _, size := range []int{64, 65} {
			t.Run(fmt.Sprintf("%s/%d", stream, size), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				spec := helper(t, "output", stream, strconv.Itoa(size))
				spec.OutputLimit = 64
				r, err := Run(ctx, spec)
				if size == 64 && err != nil {
					t.Fatal(err)
				}
				if size > 64 && !errors.Is(err, ErrOutputLimit) {
					t.Fatalf("expected explicit output-limit error, got %v", err)
				}
				if len(r.Stdout) > 64 || len(r.Stderr()) > 64 {
					t.Fatal("output exceeded capture bound")
				}
			})
		}
	}
}

func TestCancellationEscalatesAndCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := Start(ctx, helper(t, "ignore"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if line, err := bufio.NewReader(p.Stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("child not ready")
	}
	cancel()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if _, err := p.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineAndStartupFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, helper(t, "wait")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
	missing := helper(t, "echo")
	missing.Name = filepath.Join(t.TempDir(), "missing-executable")
	if _, err := Start(context.Background(), missing); err == nil {
		t.Fatal("missing executable succeeded")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, helper(t, "echo")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bad := helper(t, "echo")
	bad.Dir = "relative"
	if _, err := Start(context.Background(), bad); err == nil {
		t.Fatal("relative working directory accepted")
	}
	bad = helper(t, "echo")
	bad.GracePeriod = -time.Second
	if _, err := Start(context.Background(), bad); err == nil {
		t.Fatal("negative shutdown period accepted")
	}
	bad = helper(t, "echo")
	bad.OutputLimit = -1
	if _, err := Start(context.Background(), bad); err == nil {
		t.Fatal("negative output limit accepted")
	}
}

func TestReadinessDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	p, err := Start(ctx, helper(t, "silent"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if line, err := bufio.NewReader(p.Stdout).ReadString('\n'); err == nil || line != "" {
		t.Fatal("silent child unexpectedly became ready")
	}
	if _, err := p.Wait(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup deadline: %v", err)
	}
}

func TestCancellationCleansDescendantsAndNotOtherCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	other, err := Start(ctx, helper(t, "stream"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if line, err := bufio.NewReader(other.Stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("independent child not ready")
	}
	childCtx, stop := context.WithCancel(ctx)
	spec := helper(t, "descendant")
	spec.GracePeriod = 2 * time.Second // Allow the fixture parent to reap its worker.
	p, err := Start(childCtx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := bufio.NewReader(p.Stdout)
	if line, err := r.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("descendant not ready")
	}
	stop()
	// The descendant retains stdout. EOF without closing our own reader proves
	// the fixture worker's inherited pipe was closed by tree cleanup.
	eof := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, r); eof <- err }()
	select {
	case err := <-eof:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("owned descendant retained stdout after cancellation")
	}
	if _, err := p.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := io.WriteString(other.Stdin, "still-running\n"); err != nil {
		t.Fatal("unrelated owned command was stopped")
	}
	other.Stdin.Close()
	output, err := io.ReadAll(other.Stdout)
	if err != nil || string(output) != "reply:still-running\n" {
		t.Fatal("independent command did not complete")
	}
	if _, err := other.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("SHELDON_PROCESS_TEST_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	args := os.Args[separator:]
	// A broken supervisor must not leave a permanent fixture process behind.
	time.AfterFunc(12*time.Second, func() { os.Exit(95) })
	switch args[0] {
	case "echo":
		dir, _ := os.Getwd()
		json.NewEncoder(os.Stdout).Encode(struct {
			Dir   string
			Args  []string
			Value string
		}{dir, args[1:], os.Getenv("SHELDON_PROCESS_TEST_VALUE")})
		fmt.Fprintln(os.Stderr, "separate diagnostic")
	case "fail":
		fmt.Fprintln(os.Stderr, "provider-secret-do-not-log")
		os.Exit(7)
	case "stream":
		io.WriteString(os.Stdout, "rea")
		time.Sleep(10 * time.Millisecond)
		io.WriteString(os.Stdout, "dy\n")
		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Fprint(os.Stdout, "reply:"+input)
	case "output":
		size, _ := strconv.Atoi(args[2])
		out := os.Stdout
		if args[1] == "stderr" {
			out = os.Stderr
		}
		io.WriteString(out, strings.Repeat("x", size))
	case "ignore":
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	case "wait":
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	case "silent":
		for {
			time.Sleep(time.Hour)
		}
	case "descendant":
		termination := make(chan os.Signal, 1)
		signal.Notify(termination, os.Interrupt, syscall.SIGTERM)
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "wait")
		child.Env = os.Environ()
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if child.Start() != nil {
			os.Exit(96)
		}
		<-termination
		child.Wait() // Reap the worker after the group's termination signal.
	default:
		os.Exit(97)
	}
	os.Exit(0)
}

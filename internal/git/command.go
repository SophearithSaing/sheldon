// Package git manages restricted PR caches and pinned source worktrees.
package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/SophearithSaing/sheldon/internal/process"
)

// CommandError withholds native output and exposes only exit/cancellation evidence.
type CommandError struct {
	ExitCode int
	cause    error
}

// Error returns a fixed diagnostic without native output or remote credentials.
func (e *CommandError) Error() string {
	return "Git operation failed; check access, connectivity and the managed cache"
}

// Unwrap exposes bounded-execution causes without native diagnostics.
func (e *CommandError) Unwrap() error { return e.cause }

// run invokes fixed internal Git operations with restrictions and bounded output.
func (c *Cache) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(filepath.Join(c.controls, "hooks")); err != nil || len(entries) != 0 {
		return nil, errors.New("private Git controls changed; reopen the cache")
	}
	if info, err := os.Lstat(filepath.Join(c.controls, "attributes")); err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		return nil, errors.New("private Git controls changed; reopen the cache")
	}
	flags := []string{
		"-c", "core.hooksPath=" + filepath.Join(c.controls, "hooks"), "-c", "core.attributesFile=" + filepath.Join(c.controls, "attributes"),
		"-c", "core.fsmonitor=false", "-c", "core.askPass=", "-c", "core.sharedRepository=0600",
		"-c", "core.alternateRefsCommand=", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "fetch.writeCommitGraph=false",
		"-c", "submodule.recurse=false", "-c", "fetch.recurseSubmodules=false", "-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always", "-c", "http.sslVerify=true", "-c", "http.followRedirects=false",
	}
	var unset []string
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") && upper != "GIT_SSH" && upper != "GIT_SSH_COMMAND" && upper != "GIT_SSH_VARIANT" && upper != "GIT_CONFIG_GLOBAL" && upper != "GIT_CONFIG_SYSTEM" && upper != "GIT_CONFIG_NOSYSTEM" {
			unset = append(unset, name)
		}
	}
	result, err := process.Run(ctx, process.Command{Name: c.executable, Dir: dir, Args: append(flags, args...), UnsetEnv: unset,
		Env:         []string{"GIT_NO_REPLACE_OBJECTS=1", "GIT_ALLOW_PROTOCOL=https:ssh", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never", "SSH_ASKPASS_REQUIRE=never", "LC_ALL=C", "GIT_TRACE=0", "GIT_TRACE_PACKET=0", "GIT_TRACE_PACK_ACCESS=0", "GIT_TRACE_PERFORMANCE=0", "GIT_TRACE_SETUP=0", "GIT_TRACE_SHALLOW=0", "GIT_TRACE_CURL=0", "GIT_TRACE2=0", "GIT_TRACE2_EVENT=0", "GIT_TRACE2_PERF=0"},
		OutputLimit: 4 << 20, GracePeriod: 100 * time.Millisecond})
	if err != nil {
		var cause error
		for _, safe := range []error{context.Canceled, context.DeadlineExceeded, process.ErrOutputLimit} {
			if errors.Is(err, safe) {
				cause = safe
				break
			}
		}
		return nil, &CommandError{ExitCode: result.ExitCode, cause: cause}
	}
	return result.Stdout, nil
}

// gitExecutable resolves a native executable without admitting shell shims.
func gitExecutable() (string, error) {
	path, err := exec.LookPath("git")
	ext := strings.ToLower(filepath.Ext(path))
	if err != nil || !filepath.IsAbs(path) || ext == ".cmd" || ext == ".bat" || ext == ".ps1" || runtime.GOOS != "windows" && ext == ".exe" {
		return "", errors.New("a native Git executable on an absolute PATH is required")
	}
	return path, nil
}

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestRun(t *testing.T) {
	for _, tt := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
		starts bool
		url    string
	}{
		{name: "no arguments", starts: true},
		{name: "help", args: []string{"--help"}, stdout: usage},
		{name: "short help", args: []string{"-h"}, stdout: usage},
		{name: "version", args: []string{"--version"}, stdout: "sheldon " + version + "\n"},
		{
			name: "unknown flag", args: []string{"--unknown"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for available commands.\n",
		},
		{
			name: "open", args: []string{"open", "https://github.com/owner/repo/pull/1?tab=files#discussion"}, starts: true,
			url: "https://github.com/owner/repo/pull/1",
		},
		{
			name: "invalid open", args: []string{"open", "https://attacker.example/owner/repo/pull/1"}, code: 2,
			stderr: "expected https://github.com/<owner>/<repo>/pull/<positive-number>\n",
		},
		{
			name: "missing URL", args: []string{"open"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for available commands.\n",
		},
		{
			name: "extra arguments", args: []string{"--help", "extra"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for available commands.\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			called := false
			start := func(ctx context.Context, url string, out, errOut io.Writer) error {
				called = true
				if url != tt.url || out != &stdout || errOut != &stderr {
					t.Fatal("incorrect runtime arguments")
				}
				return nil
			}
			if code := run(context.Background(), tt.args, &stdout, &stderr, start); code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			if called != tt.starts {
				t.Fatalf("runtime called=%v, want %v", called, tt.starts)
			}
			if stdout.String() != tt.stdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.stdout)
			}
			if stderr.String() != tt.stderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.stderr)
			}
		})
	}
}

func TestRunRuntimeFailureAndCancellation(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code int
	}{
		{errors.New("runtime unavailable"), 1},
		{context.Canceled, 0},
	} {
		var stdout, stderr bytes.Buffer
		start := func(context.Context, string, io.Writer, io.Writer) error { return tt.err }
		if code := run(context.Background(), nil, &stdout, &stderr, start); code != tt.code {
			t.Fatalf("code %d, want %d", code, tt.code)
		}
	}
}

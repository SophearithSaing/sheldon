package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	for _, tt := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{name: "no arguments", stdout: usage},
		{name: "help", args: []string{"--help"}, stdout: usage},
		{name: "short help", args: []string{"-h"}, stdout: usage},
		{name: "version", args: []string{"--version"}, stdout: "sheldon " + version + "\n"},
		{
			name: "unknown flag", args: []string{"--unknown"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for the available scaffold commands.\n",
		},
		{
			name: "open not implemented", args: []string{"open", "https://github.com/owner/repo/pull/1"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for the available scaffold commands.\n",
		},
		{
			name: "extra arguments", args: []string{"--help", "extra"}, code: 2,
			stderr: "Unsupported command or arguments. Run sheldon --help for the available scaffold commands.\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
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

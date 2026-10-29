//go:build linux || darwin

package app

import (
	"context"
	"os/exec"
	"runtime"
)

// openBrowser hands a local URL to the desktop without a shell or captured output.
// The external browser is user-owned, not an owned background service to kill.
func openBrowser(ctx context.Context, target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(ctx, name, target).Run()
}

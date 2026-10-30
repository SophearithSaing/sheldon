//go:build linux || darwin

package app

import (
	"context"
	"os/exec"
	"runtime"
)

// openBrowser opens target in the default desktop browser.
func openBrowser(ctx context.Context, target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(ctx, name, target).Run()
}

package app

import (
	"context"

	"golang.org/x/sys/windows"
)

// openBrowser delegates a local URL to Windows' URL handler without cmd.exe.
func openBrowser(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	u, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}

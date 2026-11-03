//go:build linux || darwin

package git

import (
	"errors"
	"os"
	"syscall"
)

// privateDirectory creates private cache storage without repairing existing modes.
func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return checkEntry(path, true)
}

// checkEntry refuses unsafe file types, ownership, permissions and linked files.
func checkEntry(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.IsDir() != directory || !directory && (!info.Mode().IsRegular() || stat.Nlink != 1) {
		return errors.New("Git cache entries must be private, current-user-owned directories or unlinked regular files")
	}
	return nil
}

//go:build linux || darwin

package app

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// privateDirectory creates an owner-only data directory or rejects unsafe storage.
func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !privateOwner(info) || info.Mode().Perm()&0077 != 0 {
		return errors.New("Sheldon data directory must be owned by the current user, private (0700), and not a symlink")
	}
	return nil
}

// privateOwner checks ownership using descriptor metadata rather than path names.
func privateOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// privateFile opens a private regular file, refusing symlinks, hard links and FIFOs.
func privateFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !privateOwner(info) || info.Mode().Perm() != 0600 || !ok || stat.Nlink != 1 {
		f.Close()
		return nil, errors.New("instance record must be a private (0600), current-user-owned regular file")
	}
	return f, nil
}

// lockFile acquires a nonblocking advisory lock released automatically on exit.
func lockFile(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

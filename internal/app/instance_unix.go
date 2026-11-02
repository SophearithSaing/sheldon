//go:build linux || darwin

package app

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// privateDirectory creates an owner-only data directory.
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

// privateOwner reports whether info is owned by the current user.
func privateOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// privateFile opens a private regular file, refusing links and FIFOs.
func privateFile(path string) (*os.File, error) {
	return openPrivateFile(path, true)
}

// openPrivateFile opens checked storage, optionally creating it for the instance lock.
func openPrivateFile(path string, create bool) (*os.File, error) {
	flags := unix.O_RDONLY
	if create {
		flags = unix.O_RDWR | unix.O_CREAT
	}
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err := checkPrivateFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// checkPrivateFile rejects nonprivate, linked or nonregular storage files.
func checkPrivateFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !privateOwner(info) || info.Mode().Perm() != 0600 || !ok || stat.Nlink != 1 {
		return errors.New("storage file must be a private (0600), current-user-owned regular file without links")
	}
	return nil
}

// lockFile acquires a nonblocking advisory lock.
func lockFile(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

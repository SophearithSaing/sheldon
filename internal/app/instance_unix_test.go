//go:build linux || darwin

package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateStoragePermissionsAndLinks(t *testing.T) {
	for _, kind := range []string{"directory permissions", "file permissions", "directory symlink", "file symlink", "hard link", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			paths := testPaths(t)
			if err := os.Mkdir(paths.Data, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(paths.Data, "instance.lock")
			switch kind {
			case "directory permissions":
				if err := os.Chmod(paths.Data, 0755); err != nil {
					t.Fatal(err)
				}
			case "file permissions":
				if err := os.WriteFile(file, nil, 0644); err != nil {
					t.Fatal(err)
				}
			case "directory symlink":
				if err := os.Remove(paths.Data); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), paths.Data); err != nil {
					t.Fatal(err)
				}
			case "file symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "untouched"), file); err != nil {
					t.Fatal(err)
				}
			case "hard link":
				target := filepath.Join(t.TempDir(), "untouched")
				if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, file); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(file, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if i, err := openInstance(paths.Data); err == nil {
				i.close()
				t.Fatal("unsafe storage accepted")
			}
		})
	}
	paths := testPaths(t)
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	for path, mode := range map[string]os.FileMode{paths.Data: 0700, filepath.Join(paths.Data, "instance.lock"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s does not have %o permissions", path, mode)
		}
	}
}

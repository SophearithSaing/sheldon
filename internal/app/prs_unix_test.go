//go:build linux || darwin

package app

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"golang.org/x/sys/unix"
)

func TestPRIndexRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"public", "unreadable", "symlink", "dangling symlink", "hard link", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			i := ownedPRInstance(t, testPaths(t).Data)
			dir := filepath.Dir(i.file.Name())
			path := filepath.Join(dir, "prs.json")
			target := filepath.Join(t.TempDir(), "untouched")
			original := []byte("preserve this fixture")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "public", "unreadable":
				err = os.WriteFile(path, original, 0600)
				if err == nil {
					mode := os.FileMode(0644)
					if kind == "unreadable" {
						mode = 0000
					}
					err = os.Chmod(path, mode)
				}
			case "symlink":
				err = os.Symlink(target, path)
			case "dangling symlink":
				err = os.Symlink(target+"-missing", path)
			case "hard link":
				err = os.Link(target, path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
			if got, err := i.registerPR(pr.URL); err == nil || got != (githubpr.PullRequest{}) {
				t.Fatal("unsafe index accepted for registration")
			}
			if _, err := i.readPR(pr.ID); err == nil {
				t.Fatal("unsafe index accepted for lookup")
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("external target changed")
			}
			if info, err := os.Lstat(path); err != nil || kind == "unreadable" && info.Mode().Perm() != 0000 || kind == "public" && info.Mode().Perm() != 0644 {
				t.Fatal("unsafe state removed or repaired")
			}
		})
	}
}

func TestPRIndexWriteFailurePreservesCommittedFile(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	if _, err := i.registerPR("https://github.com/owner/repo/pull/1"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Data, "prs.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("index is not private")
	}
	if err := i.close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestPRIndexWriteFailureHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_PR_WRITE_DIR="+paths.Data)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write-failure helper: %v, %s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed registration changed committed index")
	}
	reopened := ownedPRInstance(t, paths.Data)
	missing, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/2")
	if _, err := reopened.readPR(missing.ID); !os.IsNotExist(err) {
		t.Fatalf("failed registration left a phantom PR: %v", err)
	}
	if _, err := reopened.registerPR(missing.URL); err != nil {
		t.Fatalf("could not retry registration: %v", err)
	}
	entries, err := filepath.Glob(filepath.Join(paths.Data, ".prs-*.tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatal("write failure leaked temporary files")
	}
}

func TestPRIndexWriteFailureHelper(t *testing.T) {
	dir := os.Getenv("SHELDON_TEST_PR_WRITE_DIR")
	if dir == "" {
		return
	}
	i := ownedPRInstance(t, dir)
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1, Max: 1}); err != nil {
		t.Fatal(err)
	}
	pr, err := i.registerPR("https://github.com/owner/repo/pull/2")
	if err == nil || pr != (githubpr.PullRequest{}) {
		t.Fatal("failed temporary write reported registration success")
	}
}

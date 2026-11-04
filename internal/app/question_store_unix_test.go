//go:build linux || darwin

package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestQuestionStoreRejectsUnsafeParentsAndLeaves(t *testing.T) {
	for _, level := range []string{"reviews", "pr", "file"} {
		for _, kind := range []string{"symlink", "dangling", "public", "fifo", "hardlink"} {
			if kind == "hardlink" && level != "file" {
				continue
			}
			t.Run(level+"/"+kind, func(t *testing.T) {
				i := ownedPRInstance(t, testPaths(t).Data)
				s := questionStoreFixture(t, i)
				dir := filepath.Dir(i.file.Name())
				parent := filepath.Join(dir, "reviews")
				path := parent
				if level != "reviews" {
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(parent, s.PRID)
				}
				if level == "file" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(path, "threads.json")
				}
				target := filepath.Join(t.TempDir(), "untouched")
				original := []byte("private fixture content")
				if level == "file" {
					if err := os.WriteFile(target, original, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(target, path)
				case "dangling":
					err = os.Symlink(target+"-missing", path)
				case "fifo":
					err = unix.Mkfifo(path, 0600)
				case "hardlink":
					err = os.Link(target, path)
				case "public":
					if level == "file" {
						err = os.WriteFile(path, original, 0600)
						if err == nil {
							err = os.Chmod(path, 0644)
						}
					} else {
						err = os.Mkdir(path, 0700)
						if err == nil {
							err = os.Chmod(path, 0755)
						}
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := i.readQuestions(s.PRID); err == nil {
					t.Fatal("unsafe read succeeded")
				}
				if err := i.saveQuestions(s, 0); err == nil {
					t.Fatal("unsafe save succeeded")
				}
				if kind == "public" {
					info, _ := os.Lstat(path)
					if info.Mode().Perm()&0077 == 0 {
						t.Fatal("unsafe permissions repaired")
					}
				}
				if level == "file" {
					after, err := os.ReadFile(target)
					if err != nil || !bytes.Equal(original, after) {
						t.Fatal("external target changed")
					}
				} else {
					entries, _ := os.ReadDir(target)
					if len(entries) != 0 {
						t.Fatal("external directory changed")
					}
				}
			})
		}
	}
}

func TestQuestionStoreWriteFailurePreservesHistory(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(i.file.Name())
	path := filepath.Join(dir, "reviews", s.PRID, "threads.json")
	before, _ := os.ReadFile(path)
	i.close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestQuestionStoreWriteFailureHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_QUESTION_WRITE_DIR="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, output)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed write changed committed history")
	}
	entries, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".threads-*.tmp"))
	if len(entries) != 0 {
		t.Fatal("owned temporary leaked")
	}
	i = ownedPRInstance(t, dir)
	s.Version++
	if err := i.saveQuestions(s, 1); err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
}

func TestQuestionStoreWriteFailureHelper(t *testing.T) {
	dir := os.Getenv("SHELDON_TEST_QUESTION_WRITE_DIR")
	if dir == "" {
		return
	}
	i := ownedPRInstance(t, dir)
	s := questionStoreFixture(t, i)
	s.Version++
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1, Max: 1}); err != nil {
		t.Fatal(err)
	}
	if err := i.saveQuestions(s, 1); err == nil {
		t.Fatal("failed write reported success")
	}
}

func TestQuestionReplacementFailurePreservesForeignState(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("threads.json", 0700); err != nil {
		t.Fatal(err)
	}
	if err := replaceQuestionFile(root, []byte("private fixture")); !errors.Is(err, errQuestionReplace) {
		t.Fatalf("rename failure: %v", err)
	}
	info, err := root.Lstat("threads.json")
	if err != nil || !info.IsDir() {
		t.Fatal("foreign target removed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary leak")
	}
}

func TestQuestionStoreRejectsChangedDataRootAccess(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	dir := filepath.Dir(i.file.Name())
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if _, err := i.readQuestions(s.PRID); err == nil {
		t.Fatal("public data root accepted")
	}
	if err := i.saveQuestions(s, 0); err == nil {
		t.Fatal("public data root written")
	}
}

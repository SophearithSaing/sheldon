//go:build linux || darwin

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/quiz"
	"golang.org/x/sys/unix"
)

func TestQuizStoreRefusesUnsafeParentsAndLeaves(t *testing.T) {
	for _, level := range []string{"reviews", "pr", "file"} {
		for _, kind := range []string{"symlink", "dangling", "fifo", "public", "hardlink"} {
			if kind == "hardlink" && level != "file" {
				continue
			}
			t.Run(level+"/"+kind, func(t *testing.T) {
				i := ownedPRInstance(t, testPaths(t).Data)
				s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
				path := filepath.Join(filepath.Dir(i.file.Name()), "reviews")
				if level != "reviews" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(path, s.PRID)
				}
				if level == "file" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(path, "quiz.json")
				}
				target := filepath.Join(t.TempDir(), "untouched")
				original := []byte("keep external bytes")
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
				if _, err := i.ReadQuiz(s.PRID); err == nil {
					t.Fatal("unsafe read accepted")
				}
				if err := i.SaveQuiz(s, 0); err == nil {
					t.Fatal("unsafe write accepted")
				}
				if kind == "public" {
					info, _ := os.Lstat(path)
					if info.Mode().Perm()&0077 == 0 {
						t.Fatal("unsafe permissions repaired")
					}
				}
				if level == "file" {
					after, _ := os.ReadFile(target)
					if !bytes.Equal(original, after) {
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

func TestQuizStoreRealWriteFailurePreservesPreviousBytes(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	if err := i.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(i.file.Name())
	path := filepath.Join(dir, "reviews", s.PRID, "quiz.json")
	before, _ := os.ReadFile(path)
	i.close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestQuizStoreWriteFailureHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_QUIZ_WRITE_DIR="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write failure helper: %v %s", err, output)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed temporary write changed saved quiz")
	}
	entries, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".quiz-*.tmp"))
	if len(entries) != 0 {
		t.Fatal("owned temporary leaked")
	}
}

func TestQuizStoreWriteFailureHelper(t *testing.T) {
	dir := os.Getenv("SHELDON_TEST_QUIZ_WRITE_DIR")
	if dir == "" {
		return
	}
	i := ownedPRInstance(t, dir)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	s.Version = 2
	s.QuizID = strings.Repeat("9", 64)
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1, Max: 1}); err != nil {
		t.Fatal(err)
	}
	if err := i.SaveQuiz(s, 1); !errors.Is(err, quiz.ErrStore) {
		t.Fatalf("write failure reported success: %v", err)
	}
}

func TestQuizReplacementRenameFailurePreservesForeignState(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("quiz.json", 0700); err != nil {
		t.Fatal(err)
	}
	if err := replaceQuizFile(root, []byte("fixture bytes")); !errors.Is(err, quiz.ErrSaveUncertain) {
		t.Fatal("rename failure called definite success/absence")
	}
	info, err := root.Lstat("quiz.json")
	if err != nil || !info.IsDir() {
		t.Fatal("foreign state removed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("owned temporary leaked")
	}
}

func TestQuizTerminalSaveFailurePreservesOldQuizWithoutReplay(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	s := quizStoreFixture(t, f.owner, f.pr.URL)
	if err := f.owner.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.paths.Data, "reviews", f.pr.ID)
	path := filepath.Join(dir, "quiz.json")
	before, _ := os.ReadFile(path)
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizPrompts(t, f, 1)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	control, _ := json.Marshal(map[string]any{"input": 1, "outcome": "succeeded"})
	if err := os.WriteFile(f.control, control, 0600); err != nil {
		t.Fatal(err)
	}
	final := waitQuiz(t, f.owner.quizzes, admitted)
	if final.State != quiz.Failed || final.FailureCode != "save-failed" || final.SavedVersion != "" {
		t.Fatal("native completion became fake Saved")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || quizPromptCount(t, f) != 1 {
		t.Fatal("save failure changed/replayed data")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := f.owner.ReadQuiz(f.pr.ID); err != nil || got.QuizID != s.QuizID {
		t.Fatal("prior complete quiz lost")
	}
	if status, _ := f.owner.quizzes.Read(f.pr.ID, admitted.GenerationID); status.State != quiz.Failed {
		t.Fatal("storage read retried/fabricated saving")
	}
}

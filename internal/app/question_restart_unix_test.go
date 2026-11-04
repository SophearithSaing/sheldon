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

	"github.com/SophearithSaing/sheldon/internal/question"
	"golang.org/x/sys/unix"
)

func TestQuestionRestartWriteFailureIsNotHealthyHistory(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionTranscriptFixture(t, i, paths.Cache)
	if err := i.SaveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Data, "reviews", s.PRID, "threads.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i.close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestQuestionRestartWriteFailureHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_RESTART_DATA="+paths.Data, "SHELDON_TEST_RESTART_PR="+s.PRID)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart failure helper: %v %s", err, output)
	}
	i = ownedPRInstance(t, paths.Data)
	got, err := i.ReadQuestions(s.PRID)
	if err != nil || got.Version != 2 || got.Threads[0].Answers[0].FailureCode != "runtime-restarted" {
		t.Fatalf("recovered classification: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("recovered classification was not saved")
	}
}

func TestQuestionRestartWriteFailureHelper(t *testing.T) {
	dir := os.Getenv("SHELDON_TEST_RESTART_DATA")
	if dir == "" {
		return
	}
	prID := os.Getenv("SHELDON_TEST_RESTART_PR")
	i := ownedPRInstance(t, dir)
	path := filepath.Join(dir, "reviews", prID, "threads.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1, Max: limit.Max}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := i.ReadQuestions(prID)
		if err == nil || got.Version != 0 || i.questionsLoaded[prID] {
			t.Fatal("failed classification exposed healthy or initialized history")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("failed classification changed history")
		}
		entries, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".threads-*.tmp"))
		if err != nil || len(entries) != 0 {
			t.Fatal("failed classification leaked temporary files")
		}
	}
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	got, err := i.ReadQuestions(prID)
	if err != nil || got.Version != 2 || got.Threads[0].Answers[0].State != question.Interrupted || !got.Threads[0].Answers[0].ExecutionUnknown {
		t.Fatalf("retry after storage recovery: %v", err)
	}
}

func TestQuestionRestartHistoryDoesNotRunSourceOrNativeTools(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionTranscriptFixture(t, i, paths.Cache)
	if err := i.SaveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	i.close()
	i = ownedPRInstance(t, paths.Data)
	tools := t.TempDir()
	audit := filepath.Join(tools, "unexpected-execution")
	for _, tool := range []string{"git", "gh", "opencode"} {
		if err := os.WriteFile(filepath.Join(tools, tool), []byte("#!/bin/sh\nprintf 'unexpected execution\\n' >> \"$SHELDON_TEST_RESTART_AUDIT\"\nexit 91\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	t.Setenv("SHELDON_TEST_RESTART_AUDIT", audit)
	t.Setenv("OPENCODE_CONFIG_CONTENT", "invalid-config-must-not-be-used")
	got, err := i.ReadQuestions(s.PRID)
	if err != nil || got.Threads[0].Answers[0].State != question.Interrupted {
		t.Fatalf("source-free history: %v", err)
	}
	if _, err := os.Stat(audit); !os.IsNotExist(err) {
		t.Fatal("history executed source/native tool")
	}
	if _, err := os.Stat(paths.Cache); !os.IsNotExist(err) {
		t.Fatal("history created cache/worktree")
	}
	if i.questions != nil || i.cache != nil {
		t.Fatal("history initialized execution/source runtime")
	}
}

//go:build linux || darwin

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/question"
	"golang.org/x/sys/unix"
)

func TestNativeCreationWithoutSavedBindingStaysBlocked(t *testing.T) {
	f := newQuestionFixture(t, "create-gated")
	result := make(chan error, 1)
	go func() { _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(f.control + ".created"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not create native session")
		}
		time.Sleep(5 * time.Millisecond)
	}
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "threads.json")
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Bindings) != 1 || saved.Bindings[0].State != "initializing" || len(saved.Threads) != 0 {
		t.Fatal("native create preceded saved initialization")
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.control, []byte("release-create"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, question.ErrPersistence) {
			t.Fatalf("binding save failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission stalled")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); !errors.Is(err, question.ErrPersistence) {
		t.Fatalf("unsafe retry: %v", err)
	}
	entries := nativeQuestionAudit(t, f.audit)
	if countNativeSessions(entries) != 1 {
		t.Fatal("binding save failure created replacement")
	}
	for _, entry := range entries {
		if filepath.Base(entry.Path) == "prompt" {
			t.Fatal("unbound session dispatched input")
		}
	}
	f.owner.close()
	owner := ownedPRInstance(t, f.paths.Data)
	state, err := owner.readQuestions(f.pr.ID)
	if err != nil || len(state.Threads) != 0 || state.Bindings[0].State != "initializing" {
		t.Fatal("ambiguous initialization was repaired or discarded")
	}
	before := len(entries)
	service := &sourceRegistry{instance: owner, paths: f.paths}
	if _, err := owner.openCache(f.paths); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginQuestion(context.Background(), f.pr.ID, f.input); !errors.Is(err, question.ErrSessionUnavailable) {
		t.Fatalf("restart retried uncertain create: %v", err)
	}
	if len(nativeQuestionAudit(t, f.audit)) != before {
		t.Fatal("restart repeated native creation")
	}
}

func TestFollowupSaveFailureNeverDispatches(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestFollowupSaveFailureHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_ADMISSION_WRITE_FAILURE=1")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, data)
	}
}

func TestFollowupSaveFailureHelper(t *testing.T) {
	if os.Getenv("SHELDON_TEST_ADMISSION_WRITE_FAILURE") == "" {
		return
	}
	f := newQuestionFixture(t, "")
	first, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(question.Completed, "Synthetic predecessor"); err != nil {
		t.Fatal(err)
	}
	var old unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &old); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1, Max: old.Max}); err != nil {
		t.Fatal(err)
	}
	before := first.Snapshot()
	turn, admissionErr := f.service.BeginFollowup(context.Background(), f.pr.ID, before.ThreadID, question.FollowupRequest{RevisionID: before.RevisionID, Question: "Do not dispatch without saving"})
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &old); err != nil {
		t.Fatal(err)
	}
	if turn != nil || !errors.Is(admissionErr, question.ErrPersistence) {
		t.Fatalf("failed write admitted: %v", admissionErr)
	}
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Threads) != 1 || len(saved.Threads[0].Answers) != 1 {
		t.Fatal("failed save left phantom follow-up")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "prompt" {
			t.Fatal("failed admission dispatched")
		}
	}
	next, err := f.service.BeginFollowup(context.Background(), f.pr.ID, before.ThreadID, question.FollowupRequest{RevisionID: before.RevisionID, Question: "Explicit retry after restoring storage"})
	if err != nil || next.Snapshot().ThreadID != before.ThreadID {
		t.Fatalf("explicit retry: %v", err)
	}
	if countNativeSessions(nativeQuestionAudit(t, f.audit)) != 1 {
		t.Fatal("retry replaced native context")
	}
}

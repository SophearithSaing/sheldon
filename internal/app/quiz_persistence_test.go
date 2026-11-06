package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// gatedQuizStore implements the real persistence contract with an isolated acknowledgement barrier.
type gatedQuizStore struct {
	*instance
	entered     chan struct{}
	release     chan struct{}
	commitFirst bool
	uncertain   bool
}

// SaveQuiz gates a real private replacement or its acknowledgement, never native dispatch.
func (s *gatedQuizStore) SaveQuiz(next quiz.Saved, expected uint64) error {
	if s.commitFirst {
		if err := s.instance.SaveQuiz(next, expected); err != nil {
			return err
		}
	}
	close(s.entered)
	<-s.release
	if s.uncertain {
		s.quizStoreMu.Lock()
		if s.quizStoreUncertain == nil {
			s.quizStoreUncertain = map[string]bool{}
		}
		s.quizStoreUncertain[next.PRID] = true
		s.quizStoreMu.Unlock()
		return quiz.ErrSaveUncertain
	}
	if !s.commitFirst {
		return s.instance.SaveQuiz(next, expected)
	}
	return nil
}

// awaitQuizSave waits for a test-owned commit barrier rather than relying on UI timing.
func awaitQuizSave(t *testing.T, store *gatedQuizStore) {
	t.Helper()
	select {
	case <-store.entered:
	case <-time.After(8 * time.Second):
		t.Fatal("save barrier not reached")
	}
}

func TestQuizSavingGatesCompletionAndLateCancellation(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	previous := quizStoreFixture(t, f.owner, f.pr.URL)
	if err := f.owner.SaveQuiz(previous, 0); err != nil {
		t.Fatal(err)
	}
	store := &gatedQuizStore{instance: f.owner, entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-store.release:
		default:
			close(store.release)
		}
	})
	runtime := quiz.New(store, f.owner.cache)
	f.owner.quizzes = runtime
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizSave(t, store)
	status, err := runtime.Read(f.pr.ID, admitted.GenerationID)
	if err != nil || status.State != quiz.Saving || status.SavedVersion != "" {
		t.Fatal("native completion fabricated saving success")
	}
	if old, err := f.owner.ReadQuiz(f.pr.ID); err != nil || old.QuizID != previous.QuizID {
		t.Fatal("previous set unavailable while committing")
	}
	status, err = runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID)
	if err != nil || status.CancelRequested || status.State != quiz.Saving {
		t.Fatal("late Cancel promised to prevent an entered commit")
	}
	close(store.release)
	final := waitQuiz(t, runtime, admitted)
	if final.State != quiz.Completed || final.SavedVersion != "2" {
		t.Fatalf("save completion: %+v", final)
	}
	if saved, err := f.owner.ReadQuiz(f.pr.ID); err != nil || saved.QuizID != admitted.QuizID || saved.Version != 2 {
		t.Fatal("completed set not actually saved")
	}
}

func TestQuizUncertainSaveRequiresExplicitReadWithoutInferenceReplay(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	store := &gatedQuizStore{instance: f.owner, entered: make(chan struct{}), release: make(chan struct{}), commitFirst: true, uncertain: true}
	t.Cleanup(func() {
		select {
		case <-store.release:
		default:
			close(store.release)
		}
	})
	runtime := quiz.New(store, f.owner.cache)
	f.owner.quizzes = runtime
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizSave(t, store)
	close(store.release)
	final := waitQuiz(t, runtime, admitted)
	if final.State != quiz.Failed || final.FailureCode != "save-uncertain" || final.SavedVersion != "" || final.ExecutionUnknown {
		t.Fatal("storage uncertainty misclassified as saved/native unknown")
	}
	if _, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1); !errors.Is(err, quiz.ErrSaveUncertain) {
		t.Fatal("admission silently cleared storage uncertainty")
	}
	if quizPromptCount(t, f) != 1 {
		t.Fatal("uncertain save replayed inference")
	}
	saved, err := f.owner.ReadQuiz(f.pr.ID)
	if err != nil || saved.QuizID != admitted.QuizID || saved.Version != 1 {
		t.Fatal("explicit read could not establish actual committed set")
	}
	if err := f.owner.CheckQuiz(f.pr.ID, 1); err != nil {
		t.Fatal("checked read did not release storage-only gate")
	}
	if status, _ := runtime.Read(f.pr.ID, admitted.GenerationID); status.State != quiz.Failed {
		t.Fatal("read fabricated an acknowledged successful generation")
	}
}

func TestQuizShutdownWaitsForExistingCommitWithoutAdditionalWrites(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	store := &gatedQuizStore{instance: f.owner, entered: make(chan struct{}), release: make(chan struct{}), commitFirst: true}
	t.Cleanup(func() {
		select {
		case <-store.release:
		default:
			close(store.release)
		}
	})
	runtime := quiz.New(store, f.owner.cache)
	f.owner.quizzes = runtime
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizSave(t, store)
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "quiz.json")
	before, _ := os.ReadFile(path)
	done := make(chan error, 1)
	go func() { done <- f.owner.close() }()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		f.owner.mu.Lock()
		closing := f.owner.closing
		f.owner.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("writer did not close")
		case <-tick.C:
		}
	}
	select {
	case <-done:
		t.Fatal("writer released before in-flight save acknowledgement")
	default:
	}
	close(store.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-deadline.C:
		t.Fatal("writer failed to drain")
	}
	if status, _ := runtime.Read(f.pr.ID, admitted.GenerationID); status.State != quiz.Completed || status.SavedVersion != "1" {
		t.Fatal("existing commit was rewritten as cancelled during shutdown")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || quizPromptCount(t, f) != 1 {
		t.Fatal("shutdown changed/retried committed data")
	}
	i := ownedPRInstance(t, f.paths.Data)
	t.Setenv("PATH", "")
	if saved, err := i.ReadQuiz(f.pr.ID); err != nil || saved.QuizID != admitted.QuizID || i.cache != nil || i.quizzes != nil {
		t.Fatal("cold saved reopen started source/native work")
	}
}

func TestQuizFailuresAndCancellationPreservePreviousSavedSet(t *testing.T) {
	for _, mode := range []string{"quiz-invalid", "quiz-wait", "quiz-overflow", "quiz-failed"} {
		t.Run(mode, func(t *testing.T) {
			f := newQuestionFixture(t, mode)
			s := quizStoreFixture(t, f.owner, f.pr.URL)
			if err := f.owner.SaveQuiz(s, 0); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "quiz.json")
			before, _ := os.ReadFile(path)
			admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "quiz-wait" {
				awaitQuizPrompts(t, f, 1)
				f.owner.quizzes.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID)
			}
			if final := waitQuiz(t, f.owner.quizzes, admitted); final.State == quiz.Completed || final.SavedVersion != "" {
				t.Fatal("failed replacement claimed save")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("failed/cancelled generation changed old quiz")
			}
			if got, err := f.owner.ReadQuiz(f.pr.ID); err != nil || got.QuizID != s.QuizID {
				t.Fatal("old quiz no longer readable")
			}
		})
	}
}

func TestQuizCorruptOrStaleAdmissionDoesNotStartNativeWork(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	s := quizStoreFixture(t, f.owner, f.pr.URL)
	if err := f.owner.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0); !errors.Is(err, quiz.ErrConflict) {
		t.Fatal("stale expected version admitted")
	}
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "quiz.json")
	data := []byte("{corrupt diagnostic bytes")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1); !errors.Is(err, quiz.ErrStore) {
		t.Fatal("corrupt storage admitted generation")
	}
	if len(nativeQuestionAudit(t, f.audit)) != 0 || f.owner.quizzes != nil {
		t.Fatal("refused admission started native/runtime work")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("corrupt storage reset")
	}
}

func TestQuizLatePredecessorChangeRefusesReplacement(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizPrompts(t, f, 1)
	other := quizStoreFixture(t, f.owner, f.pr.URL)
	other.QuizID = strings.Repeat("9", 64)
	if err := f.owner.SaveQuiz(other, 0); err != nil {
		t.Fatal(err)
	}
	control, _ := json.Marshal(map[string]any{"input": 1, "outcome": "succeeded"})
	if err := os.WriteFile(f.control, control, 0600); err != nil {
		t.Fatal(err)
	}
	final := waitQuiz(t, f.owner.quizzes, admitted)
	if final.State != quiz.Failed || final.FailureCode != "quiz-changed" || final.SavedVersion != "" {
		t.Fatal("late stale candidate replaced newer set")
	}
	if saved, err := f.owner.ReadQuiz(f.pr.ID); err != nil || saved.QuizID != other.QuizID {
		t.Fatal("competing saved set lost")
	}
}

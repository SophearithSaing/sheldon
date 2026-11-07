package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/semantic"
)

type semanticMemory struct {
	owner   *instance
	mu      sync.Mutex
	content semantic.Content
	version uint64
}

// ReadPR delegates identity to the real fixture registry, not a fabricated source result.
func (s *semanticMemory) ReadPR(id string) (githubpr.PullRequest, error) { return s.owner.ReadPR(id) }

// CheckSemantic models the normal storage interface for execution-only tests.
func (s *semanticMemory) CheckSemantic(_ string, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != s.version {
		return semantic.ErrConflict
	}
	return nil
}

// SaveSemanticContent validates and acknowledges a synthetic in-memory persistence boundary.
func (s *semanticMemory) SaveSemanticContent(_, _ string, c semantic.Content, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != s.version {
		return semantic.ErrConflict
	}
	if err := c.Validate(); err != nil {
		return err
	}
	s.content = c
	s.version++
	return nil
}

// waitSemantic waits for bounded exact-generation feedback, not a native dispatch assumption.
func waitSemantic(t *testing.T, r *semantic.Runtime, s semantic.Status) semantic.Status {
	t.Helper()
	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		s, err := r.Read(s.PRID, s.GenerationID)
		if err != nil {
			t.Fatal(err)
		}
		switch s.State {
		case "completed", "failed", "cancelled", "unknown":
			return s
		}
		select {
		case <-deadline:
			t.Fatal("semantic generation did not finish")
		case <-tick.C:
		}
	}
}

func TestSemanticSeparateNativeExecution(t *testing.T) {
	for _, mode := range []string{"semantic-valid", "semantic-invalid", "semantic-failed", "semantic-feed-loss"} {
		t.Run(mode, func(t *testing.T) {
			f := newQuestionFixture(t, mode)
			store := &semanticMemory{owner: f.owner}
			r := semantic.New(store, f.owner.cache)
			defer r.Close()
			if len(nativeQuestionAudit(t, f.audit)) != 0 {
				t.Fatal("constructing runtime started native work")
			}
			s, err := r.Begin(context.Background(), f.pr.ID, f.input.Revision, 0)
			if err != nil || s.State != "pending" {
				t.Fatalf("admission: %v", err)
			}
			terminal := waitSemantic(t, r, s)
			want := "completed"
			if mode == "semantic-invalid" || mode == "semantic-failed" {
				want = "failed"
			}
			if terminal.State != want || quizPromptCount(t, f) != 1 {
				t.Fatalf("outcome: %+v", terminal)
			}
			before := len(nativeQuestionAudit(t, f.audit))
			if _, err := r.Read(s.PRID, s.GenerationID); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Cancel(s.PRID, s.GenerationID, s.RevisionID); err != nil {
				t.Fatal(err)
			}
			if len(nativeQuestionAudit(t, f.audit)) != before {
				t.Fatal("read/late cancel dispatched")
			}
			store.mu.Lock()
			version := store.version
			store.mu.Unlock()
			if (version == 1) != (want == "completed") {
				t.Fatal("failure saved content")
			}
			if f.owner.questions != nil || f.owner.quizzes != nil {
				t.Fatal("semantic execution borrowed question/quiz runtime")
			}
		})
	}
}

func TestSemanticBusyAndTargetedCancellation(t *testing.T) {
	f := newQuestionFixture(t, "semantic-gated")
	store := &semanticMemory{owner: f.owner}
	r := semantic.New(store, f.owner.cache)
	defer r.Close()
	s, err := r.Begin(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizPrompts(t, f, 1)
	if _, err := r.Begin(context.Background(), f.pr.ID, f.input.Revision, 0); !errors.Is(err, semantic.ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	if _, err := r.Cancel(s.PRID, s.GenerationID, s.RevisionID); err != nil {
		t.Fatal(err)
	}
	if end := waitSemantic(t, r, s); end.State != "cancelled" || end.SavedVersion != "" {
		t.Fatalf("cancel: %+v", end)
	}
	if quizPromptCount(t, f) != 1 || store.version != 0 {
		t.Fatal("cancel replayed or saved")
	}
}

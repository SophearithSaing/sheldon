package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
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

func TestSemanticSelectedQuestionReusesOriginalSharedSession(t *testing.T) {
	f := newQuestionFixture(t, "semantic-valid")
	first, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	finishNativeTurn(t, f, first)
	before, err := f.owner.ReadQuestions(f.pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.service.GenerateSemantic(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitSemantic(t, f.owner.semantics, a); end.State != "completed" {
		t.Fatalf("generation: %+v", end)
	}
	saved, err := f.owner.ReadSemantic(f.pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	anchor := saved.Groups[0].Ranges[0]
	after, err := f.owner.ReadQuestions(f.pr.ID)
	if err != nil || after.Version != before.Version {
		t.Fatal("group generation mutated question history")
	}
	input := question.InitialRequest{Revision: saved.Diff.Revision, Anchor: question.Anchor{Kind: "lines", FileID: anchor.FileID, HunkID: anchor.HunkID, Side: anchor.Side, Start: anchor.Start, End: anchor.End}, Question: "Explain the grouped selection"}
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	finishNativeTurn(t, f, turn)
	history, err := f.owner.ReadQuestions(f.pr.ID)
	if err != nil || len(history.Bindings) != 1 || len(history.Threads) != 2 {
		t.Fatalf("question session/threads: %v", err)
	}
	if history.Threads[1].Context.Anchor != input.Anchor || history.Threads[1].Context.Selected[0].Text != "old literal" {
		t.Fatal("semantic question lost original old-side context")
	}
	creates := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if entry.Method == "POST" && entry.Path == "/api/session" {
			creates++
		}
	}
	if creates != 2 || quizPromptCount(t, f) != 3 {
		t.Fatalf("expected separate generation and one shared question session, creates=%d", creates)
	}
}

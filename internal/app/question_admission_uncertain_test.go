package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

type admissionRegistry struct {
	owner       *instance
	writes      int
	uncertainAt int
	cancel      context.CancelFunc
}

// ReadPR uses the actual writer-owned registry for the offline admission fixture.
func (a *admissionRegistry) ReadPR(id string) (githubpr.PullRequest, error) {
	return a.owner.ReadPR(id)
}

// ReadQuestions reads actual private files, not a fabricated saved transcript.
func (a *admissionRegistry) ReadQuestions(id string) (question.Saved, error) {
	return a.owner.ReadQuestions(id)
}

// SaveQuestions models lost save acknowledgement after a real successful replacement.
func (a *admissionRegistry) SaveQuestions(s question.Saved, previous uint64) error {
	if err := a.owner.SaveQuestions(s, previous); err != nil {
		return err
	}
	a.writes++
	if a.writes == 3 && a.cancel != nil {
		a.cancel()
	}
	if a.writes == a.uncertainAt {
		return question.ErrSaveUncertain
	}
	return nil
}

func TestUncertainSavedAdmissionNeverAuthorizesDispatchOrReplacement(t *testing.T) {
	for _, checkpoint := range []int{1, 2, 3} {
		t.Run(string(rune('0'+checkpoint)), func(t *testing.T) {
			f := newQuestionFixture(t, "")
			registry := &admissionRegistry{owner: f.owner, uncertainAt: checkpoint}
			r := question.New(registry, f.owner.cache)
			t.Cleanup(func() { r.Close() })
			if turn, err := r.BeginQuestion(context.Background(), f.pr.ID, f.input); turn != nil || !errors.Is(err, question.ErrSaveUncertain) {
				t.Fatalf("uncertain save admitted: %v", err)
			}
			if turn, err := r.BeginQuestion(context.Background(), f.pr.ID, f.input); turn != nil || !errors.Is(err, question.ErrSaveUncertain) {
				t.Fatalf("uncertain save retried: %v", err)
			}
			saved, err := f.owner.readQuestions(f.pr.ID)
			if err != nil || len(saved.Bindings) != 1 {
				t.Fatalf("lost committed binding: %v", err)
			}
			if checkpoint == 3 {
				if len(saved.Threads) != 1 || len(saved.Threads[0].Answers) != 1 {
					t.Fatal("lost durable pending turn")
				}
				a := saved.Threads[0].Answers[0]
				if _, err := r.ReadAnswer(f.pr.ID, a.ThreadID, a.ID, question.AnswerRequest{RevisionID: a.RevisionID}); !errors.Is(err, question.ErrNotFound) {
					t.Fatal("uncertain result published a phantom runtime thread")
				}
			} else if len(saved.Threads) != 0 {
				t.Fatal("binding-only save invented a transcript")
			}
			expected := 1
			if checkpoint == 1 {
				expected = 0
			}
			entries := nativeQuestionAudit(t, f.audit)
			if countNativeSessions(entries) != expected {
				t.Fatal("uncertainty created competing sessions")
			}
			for _, entry := range entries {
				if filepath.Base(entry.Path) == "prompt" {
					t.Fatal("uncertain save dispatched input")
				}
			}
		})
	}
}

func TestCancellationAfterSavedAdmissionDoesNotDispatch(t *testing.T) {
	f := newQuestionFixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := &admissionRegistry{owner: f.owner, cancel: cancel}
	r := question.New(registry, f.owner.cache)
	defer r.Close()
	if turn, err := r.BeginQuestion(ctx, f.pr.ID, f.input); turn != nil || !errors.Is(err, question.ErrSaveUncertain) {
		t.Fatalf("cancelled persisted admission: %v", err)
	}
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Threads) != 1 || saved.Threads[0].Answers[0].State != question.Pending {
		t.Fatal("cancelled pending admission was erased or completed")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "prompt" {
			t.Fatal("cancelled admission dispatched input")
		}
	}
}

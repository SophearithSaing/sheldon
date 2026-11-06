package quiz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// runtimeFixture creates private status state for deterministic scope/terminal tests only.
func runtimeFixture(t *testing.T) (*Runtime, *generation) {
	t.Helper()
	prepared, _, _ := sourceFixture(t)
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	r := New(fixtureRegistry{pr: pr}, &git.Cache{})
	t.Cleanup(func() { r.Close() })
	id, _ := prepared.Revision.ID()
	g := &generation{status: Status{PRID: pr.ID, GenerationID: strings.Repeat("1", 64), QuizID: strings.Repeat("2", 64), RevisionID: id, Sequence: "1", State: Pending}, revision: prepared.Revision, sequence: 1, stop: make(chan struct{})}
	r.generations[pr.ID] = g
	return r, g
}

func TestRuntimeRejectsClosedBusyUnknownAndCapacityWithoutStartingWork(t *testing.T) {
	for _, name := range []string{"busy", "unknown", "closed", "capacity", "cancelled request", "invalid revision", "unregistered"} {
		t.Run(name, func(t *testing.T) {
			r, g := runtimeFixture(t)
			want := ErrBusy
			ctx, revision := context.Background(), g.revision
			switch name {
			case "unknown":
				r.finish(g, Unknown, "execution-uncertain", Content{})
				want = ErrUnknown
			case "closed":
				r.Close()
				want = ErrClosed
			case "capacity":
				delete(r.generations, g.status.PRID)
				for i := range maxGenerationPRs {
					r.generations[fmt.Sprintf("%064x", i+1)] = &generation{}
				}
				want = ErrLimit
			case "cancelled request":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "invalid revision":
				revision.PRID = strings.Repeat("a", 64)
				want = ErrSource
			case "unregistered":
				r.registry = fixtureRegistry{err: errors.New("private registry diagnostic")}
				want = ErrSource
			}
			status, err := r.Begin(ctx, g.status.PRID, revision)
			if !errors.Is(err, want) || status != (Status{}) || r.service != nil || strings.Contains(err.Error(), "diagnostic") {
				t.Fatalf("refusal started work/leaked diagnostics: %v", err)
			}
		})
	}
}

func TestRuntimeExactScopeCancellationAndUnknownRemainSeparate(t *testing.T) {
	r, g := runtimeFixture(t)
	for _, input := range [][3]string{
		{"../foreign", g.status.GenerationID, g.status.RevisionID},
		{strings.Repeat("a", 64), g.status.GenerationID, g.status.RevisionID},
		{g.status.PRID, strings.Repeat("b", 64), g.status.RevisionID},
		{g.status.PRID, g.status.GenerationID, strings.Repeat("c", 64)},
	} {
		if _, err := r.Cancel(input[0], input[1], input[2]); !errors.Is(err, ErrNotFound) || g.stopped {
			t.Fatal("wrong-scope cancellation affected retained work")
		}
	}
	stopped, err := r.Cancel(g.status.PRID, g.status.GenerationID, g.status.RevisionID)
	if err != nil || !stopped.CancelRequested || stopped.State != Pending || stopped.Sequence != "2" || stopped.ExecutionUnknown {
		t.Fatal("acknowledgement became terminal/unknown proof")
	}
	r.finish(g, Unknown, "session-uncertain", Content{})
	stopped, err = r.Cancel(g.status.PRID, g.status.GenerationID, g.status.RevisionID)
	if err != nil || stopped.State != Unknown || !stopped.ExecutionUnknown || stopped.Sequence != "3" {
		t.Fatal("late Cancel erased uncertain creation/execution")
	}
	if _, err := r.Content(g.status.PRID, g.status.GenerationID); !errors.Is(err, ErrNotReady) {
		t.Fatal("unknown generation exposed a candidate")
	}
	if _, err := r.Read(g.status.PRID, "../invalid"); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid read scope accepted")
	}
}

func TestRuntimeCancellationBeforeValidationDiscardsContent(t *testing.T) {
	r, g := runtimeFixture(t)
	if _, err := r.Cancel(g.status.PRID, g.status.GenerationID, g.status.RevisionID); err != nil {
		t.Fatal(err)
	}
	r.finish(g, Validated, "", Content{Questions: []Question{{Prompt: "private discarded result"}}})
	if g.status.State != Cancelled || len(g.content.Questions) != 0 || !g.finished {
		t.Fatal("cancelled candidate was published")
	}
	if r.proceed(g, Generating) {
		t.Fatal("cancelled preparation proceeded")
	}
}

func TestRuntimeDeadlineIsFixedFeedbackNotNativeCompletion(t *testing.T) {
	r, g := runtimeFixture(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r.failPreparation(g, ctx, context.DeadlineExceeded)
	if g.status.State != Failed || g.status.FailureCode != "generation-timeout" || g.status.ExecutionUnknown || !g.finished {
		t.Fatal("known pre-dispatch deadline was misclassified")
	}
}

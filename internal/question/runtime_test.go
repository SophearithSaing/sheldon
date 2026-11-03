package question

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// runtimeTurn constructs runtime state only; its answers are explicitly synthetic, not inference.
func runtimeTurn(t *testing.T) (*Runtime, *Turn) {
	t.Helper()
	registry, input, identity, _ := fixture(t)
	r := New(registry, nil)
	t.Cleanup(func() { r.Close() })
	revisionID, _ := input.Revision.ID()
	b := &binding{busy: true}
	turn := &Turn{runtime: r, binding: b, answer: Answer{ID: strings.Repeat("1", 64), ThreadID: identity.ID, PRID: registry.pr.ID, RevisionID: revisionID, Question: "fixture question", State: Pending}}
	b.active = turn
	r.bindings[revisionKey{registry.pr.ID, revisionID}] = b
	r.threads[identity.ID] = &thread{context: identity, anchor: input.Anchor, answers: []*Turn{turn}}
	r.answers[turn.answer.ID] = turn
	r.threadCount, r.answerCount = 1, 1
	return r, turn
}

func TestConfirmedTerminalOutcomesReleaseOnlyTheirOwnSlot(t *testing.T) {
	for _, state := range []State{Completed, Failed, Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			r, turn := runtimeTurn(t)
			if err := turn.Finish(state, "synthetic fixture answer"); err != nil || turn.binding.busy || turn.binding.active != nil {
				t.Fatal("known terminal answer retained its slot")
			}
			if err := turn.Finish(state, "synthetic fixture answer"); err != nil {
				t.Fatal("identical terminal update was not idempotent")
			}
			other := &Turn{runtime: r, binding: turn.binding, answer: Answer{ID: strings.Repeat("2", 64), State: Pending}}
			turn.binding.busy, turn.binding.active = true, other
			if err := turn.Finish(state, "synthetic fixture answer"); err != nil || turn.binding.active != other || !turn.binding.busy {
				t.Fatal("late terminal update released a newer answer")
			}
			if err := turn.Finish(Failed, "conflicting"); !errors.Is(err, ErrAnswerState) {
				t.Fatal("terminal answer was rewritten")
			}
		})
	}
}

func TestUncertainAnswerKeepsSharedSessionUnavailable(t *testing.T) {
	r, turn := runtimeTurn(t)
	if err := turn.Uncertain(); err != nil || turn.Snapshot().State != Interrupted || !turn.binding.busy {
		t.Fatal("uncertainty appeared idle")
	}
	key := revisionKey{turn.answer.PRID, turn.answer.RevisionID}
	if _, err := r.reserve(key, true); !errors.Is(err, ErrUncertain) {
		t.Fatal("uncertain session admitted another question")
	}
	if err := turn.Finish(Interrupted, "preserved partial"); err != nil || turn.binding.broken != nil || turn.binding.busy {
		t.Fatal("confirmed idle outcome did not resolve its own uncertainty")
	}
}

func TestRuntimeAnswerScopeAndPrivateFormatting(t *testing.T) {
	r, turn := runtimeTurn(t)
	snapshot := turn.Snapshot()
	got, err := r.ReadAnswer(snapshot.PRID, snapshot.ThreadID, snapshot.ID, AnswerRequest{RevisionID: snapshot.RevisionID})
	if err != nil || got != snapshot {
		t.Fatal("answer was not routed to its submitting thread")
	}
	for _, change := range []func(*Answer){
		func(a *Answer) { a.PRID = strings.Repeat("3", 64) },
		func(a *Answer) { a.ThreadID = strings.Repeat("3", 64) },
		func(a *Answer) { a.ID = strings.Repeat("3", 64) },
		func(a *Answer) { a.RevisionID = strings.Repeat("3", 64) },
	} {
		wrong := snapshot
		change(&wrong)
		if _, err := r.ReadAnswer(wrong.PRID, wrong.ThreadID, wrong.ID, AnswerRequest{RevisionID: wrong.RevisionID}); err == nil {
			t.Fatal("cross-context answer lookup accepted")
		}
	}
	for _, value := range []any{r, turn} {
		data, _ := json.Marshal(value)
		if string(data) != "{}" || strings.Contains(fmt.Sprintf("%v %+v %#v", value, value, value), snapshot.Question) {
			t.Fatal("runtime internals leaked through serialization/formatting")
		}
	}
	for _, update := range []struct {
		state State
		text  string
	}{{Pending, "not terminal"}, {Completed, " "}, {Failed, string([]byte{0xff})}, {Completed, strings.Repeat("x", maxAnswerBytes+1)}} {
		if err := turn.Finish(update.state, update.text); !errors.Is(err, ErrAnswerState) {
			t.Fatal("invalid answer state or output accepted")
		}
	}
}

func TestPreparationFailureHasNoPhantomThreadOrNativeStartup(t *testing.T) {
	registry, input, _, _ := fixture(t)
	r := New(registry, nil)
	defer r.Close()
	for range 3 {
		if turn, err := r.BeginQuestion(context.Background(), registry.pr.ID, input); err == nil || turn != nil {
			t.Fatal("missing source created a question")
		}
	}
	if len(r.bindings) != 0 || len(r.answers) != 0 || len(r.threads) != 0 || r.threadCount != 0 || r.answerCount != 0 {
		t.Fatal("preparation failure left phantom runtime state")
	}
	select {
	case <-r.serviceReady:
		t.Fatal("source failure started the native service")
	default:
	}
}

func TestRuntimeCapacityAndClosePreserveExistingAnswers(t *testing.T) {
	r, turn := runtimeTurn(t)
	if err := turn.Finish(Completed, "fixture answer"); err != nil {
		t.Fatal(err)
	}
	r.answerCount = maxAnswers
	if _, err := r.reserve(revisionKey{turn.answer.PRID, turn.answer.RevisionID}, true); !errors.Is(err, ErrCapacity) {
		t.Fatal("answer capacity was not enforced")
	}
	if turn.Snapshot().Text != "fixture answer" {
		t.Fatal("capacity failure discarded history")
	}
	r.answerCount = 1
	r.threadCount = maxThreads
	if _, err := r.reserve(revisionKey{turn.answer.PRID, turn.answer.RevisionID}, true); !errors.Is(err, ErrCapacity) {
		t.Fatal("thread capacity was not enforced")
	}
	r.threadCount = 1
	for n := range maxBindings {
		r.bindings[revisionKey{fmt.Sprint(n), "fixture"}] = &binding{}
	}
	if _, err := r.reserve(revisionKey{"new", "fixture"}, true); !errors.Is(err, ErrCapacity) {
		t.Fatal("binding capacity was not enforced")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reserve(revisionKey{turn.answer.PRID, turn.answer.RevisionID}, true); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatal("closed runtime admitted work")
	}
}

func TestThreadPromptIncludesOnlyPinnedSourceAndRelevantHistory(t *testing.T) {
	prepared, diff, anchor := selectionFixture(t)
	source, err := selectContext(prepared, diff, anchor)
	if err != nil {
		t.Fatal(err)
	}
	r, turn := runtimeTurn(t)
	turn.Finish(Completed, "synthetic prior answer")
	existing := r.threads[turn.answer.ThreadID]
	prompt, err := questionPrompt(source, existing.context.ID, "next-answer", "current follow-up", existing)
	if err != nil || strings.Contains(prompt, prepared.Directory) || !strings.Contains(prompt, "synthetic prior answer") || !strings.Contains(prompt, source.Revision.HeadSHA) || !strings.Contains(prompt, source.Revision.MergeBaseSHA) {
		t.Fatal("prompt lost scoped history/source or included a backend path")
	}
	first, err := questionPrompt(source, "other-thread", "other-answer", "new question", nil)
	if err != nil || strings.Contains(first, "synthetic prior answer") {
		t.Fatal("new UI thread copied another thread's transcript")
	}
	turn.answer.Text = strings.Repeat("x", maxPromptBytes)
	if _, err := questionPrompt(source, existing.context.ID, "next", "follow-up", existing); !errors.Is(err, ErrPromptLimit) {
		t.Fatal("oversized history was accepted or silently truncated")
	}
}

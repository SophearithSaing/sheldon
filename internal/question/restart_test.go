package question

import (
	"reflect"
	"strings"
	"testing"
)

func TestInterruptUnfinishedPreservesSavedHistory(t *testing.T) {
	for _, state := range []State{Pending, Streaming, Completed, Failed, Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			previous := savedFixture()
			previous.Threads[0].Answers[0].State = state
			original := cloneSaved(t, previous)
			next, changed := previous.InterruptUnfinished()
			wantChanged := state == Pending || state == Streaming
			if changed != wantChanged || !reflect.DeepEqual(previous, original) {
				t.Fatal("classification changed its input or wrong states")
			}
			expected := cloneSaved(t, original)
			if wantChanged {
				expected.Threads[0].Answers[0].State = Interrupted
				expected.Threads[0].Answers[0].ExecutionUnknown = true
				expected.Threads[0].Answers[0].FailureCode = "runtime-restarted"
			}
			if !reflect.DeepEqual(next, expected) {
				t.Fatal("classification lost text or immutable history")
			}
			if next.Validate(next.PRID) != nil {
				t.Fatal("classified snapshot is invalid")
			}
			again, repeated := next.InterruptUnfinished()
			if repeated || !reflect.DeepEqual(again, next) {
				t.Fatal("classification is not idempotent")
			}
			if wantChanged {
				next.Version++
				if next.ValidateUpdate(previous) != nil {
					t.Fatal("restart transition cannot be saved")
				}
			}
		})
	}
}

func TestRestartReasonRequiresUnknownInterruption(t *testing.T) {
	for _, state := range []State{Pending, Streaming, Completed, Failed, Interrupted} {
		s := savedFixture()
		s.Threads[0].Answers[0].State = state
		s.Threads[0].Answers[0].FailureCode = "runtime-restarted"
		if s.Validate(s.PRID) == nil {
			t.Fatalf("accepted restart reason as %s without unknown execution", state)
		}
	}
}

func TestInterruptUnfinishedPreservesTerminalPredecessors(t *testing.T) {
	s := savedFixture()
	answer := s.Threads[0].Answers[0]
	for n, state := range []State{Failed, Interrupted, Streaming} {
		next := answer
		next.ID = strings.Repeat(string(rune('4'+n)), 64)
		next.State = state
		s.Threads[0].Answers = append(s.Threads[0].Answers, next)
	}
	if err := s.Validate(s.PRID); err != nil {
		t.Fatal(err)
	}
	next, changed := s.InterruptUnfinished()
	if !changed || !reflect.DeepEqual(s.Threads[0].Answers[:3], next.Threads[0].Answers[:3]) {
		t.Fatal("restart rewrote terminal predecessors")
	}
	if next.Threads[0].Answers[3].State != Interrupted || !next.Threads[0].Answers[3].ExecutionUnknown {
		t.Fatal("unfinished final turn was not classified")
	}
}

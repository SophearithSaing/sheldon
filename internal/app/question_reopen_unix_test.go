//go:build linux || darwin

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/question"
)

func TestRestartInterruptedHistoryContinuesOnlyAfterExactNativeIdleProof(t *testing.T) {
	f := newQuestionFixture(t, "answer-browser")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := turn.Start(); err != nil {
		t.Fatal(err)
	}
	a := turn.Snapshot()
	s := questionHTTP(t, f)
	observeHTTP(t, s, a, true)
	path := filepath.Join(f.paths.Data, "reviews", a.PRID, "threads.json")
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if err := os.WriteFile(f.control, []byte(`{"input":1,"outcome":"succeeded"}`), 0600); err != nil {
		t.Fatal(err)
	}
	failed := observeHTTP(t, s, a, false)
	if failed.State != question.Completed || failed.Persistence != "failed" {
		t.Fatalf("fixture did not retain unsaved native completion: %+v", failed)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	f = restartQuestionFixture(t, f)
	t.Setenv("SHELDON_NATIVE_MODE", "")
	saved, err := f.owner.ReadQuestions(a.PRID)
	if err != nil || saved.Threads[0].Answers[0].State != question.Interrupted || !saved.Threads[0].Answers[0].ExecutionUnknown || saved.Threads[0].Answers[0].Text != "" {
		t.Fatal("restart fabricated a saved completion")
	}
	before := nativePromptCount(t, f)
	turn, err = f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "New explicit question after native idle validation"})
	if err != nil {
		t.Fatalf("idle original session could not continue: %v", err)
	}
	if nativePromptCount(t, f) != before {
		t.Fatal("reopening dispatched or replayed input")
	}
	finishNativeTurn(t, f, turn)
	saved, err = f.owner.ReadQuestions(a.PRID)
	if err != nil || len(saved.Threads[0].Answers) != 2 || saved.Threads[0].Answers[0].State != question.Interrupted || !saved.Threads[0].Answers[0].ExecutionUnknown || saved.Threads[0].Answers[0].Text != "" {
		t.Fatal("continuation rewrote restart-interrupted history")
	}
	f = restartQuestionFixture(t, f)
	turn, err = f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Continue after another restart"})
	if err != nil {
		t.Fatal(err)
	}
	finishNativeTurn(t, f, turn)
	if nativePromptCount(t, f) != before+2 {
		t.Fatal("restart replayed prior questions")
	}
}

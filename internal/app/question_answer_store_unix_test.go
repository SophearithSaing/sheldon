//go:build linux || darwin

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/question"
)

func TestCompletedAnswerSaveFailureRetainsTextUntilExplicitStorageRetry(t *testing.T) {
	f := newQuestionFixture(t, "answer-browser")
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
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
	if failed.State != question.Completed || failed.Persistence != "failed" || failed.Text != "Synthetic fixture answer <script>literal</script>." || failed.ExecutionUnknown {
		t.Fatalf("completed unsaved result: %+v", failed)
	}
	response := questionPost(t, context.Background(), s, "/api/reviews/"+a.PRID+"/questions", f.input)
	if response.StatusCode != 409 {
		t.Fatal("failed final save allowed another question")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	var history question.ThreadDetail
	historyResponse := historyGET(t, s, "/api/reviews/"+a.PRID+"/threads/"+a.ThreadID)
	if historyResponse.StatusCode != 200 || json.NewDecoder(historyResponse.Body).Decode(&history) != nil || history.Persistence != "failed" || history.Session.Reason != "answer-not-saved" || history.Answers[0].State != question.Pending || history.Answers[0].Text != "" {
		t.Fatal("saved read hid the unsaved runtime result or fabricated saved terminal text")
	}
	before, err := f.owner.readQuestions(a.PRID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	after, err := f.owner.readQuestions(a.PRID)
	if err != nil || after.Version != before.Version || after.Threads[0].Answers[0].State != question.Pending {
		t.Fatal("failed save was silently retried")
	}
	prompts := nativePromptCount(t, f)
	response = questionPost(t, context.Background(), s, answerPath(a)+"/save", question.AnswerRequest{RevisionID: a.RevisionID})
	var saved question.Answer
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&saved) != nil || saved.Persistence != "saved" || saved.State != question.Completed || saved.Text != failed.Text {
		t.Fatal("storage retry did not retain exact terminal text")
	}
	if nativePromptCount(t, f) != prompts {
		t.Fatal("saving replayed input")
	}
	if got := savedAnswer(t, f, a); got.State != question.Completed || got.Text != failed.Text {
		t.Fatal("terminal snapshot not committed")
	}
	if _, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Explicit follow-up after saving"}); err != nil {
		t.Fatalf("saved revision stayed blocked: %v", err)
	}
}

// nativePromptCount counts only explicit synthetic native prompt admissions.
func nativePromptCount(t *testing.T, f questionFixture) int {
	t.Helper()
	count := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "prompt" {
			count++
		}
	}
	return count
}

func TestInterruptedAnswerSaveFailurePreservesPrivateHistory(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, a, true)
	path := filepath.Join(f.paths.Data, "reviews", a.PRID, "threads.json")
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	response := questionPost(t, context.Background(), s, answerPath(a)+"/stop", question.AnswerRequest{RevisionID: a.RevisionID})
	if response.StatusCode != 202 {
		t.Fatal("Stop failed")
	}
	failed := observeHTTP(t, s, a, false)
	if failed.State != question.Interrupted || failed.Persistence != "failed" || !failed.StopRequested || failed.Text == "" {
		t.Fatalf("interrupted save failure did not retain text: %+v", failed)
	}
	interrupts := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "interrupt" {
			interrupts++
		}
	}
	if interrupts != 1 || nativePromptCount(t, f) != 1 {
		t.Fatal("save failure did not target exactly one active turn")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	response = questionPost(t, context.Background(), s, answerPath(a)+"/save", question.AnswerRequest{RevisionID: a.RevisionID})
	if response.StatusCode != 200 || nativePromptCount(t, f) != 1 {
		t.Fatal("progress storage recovery replayed input")
	}
	if got := savedAnswer(t, f, a); got.State != question.Interrupted || got.Text != failed.Text {
		t.Fatal("interrupted partial text was lost")
	}
}

func TestShutdownDoesNotSaveInProgressAnswer(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, a, true)
	before, err := f.owner.readQuestions(a.PRID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.owner.close(); err != nil {
		t.Fatalf("owned shutdown: %v", err)
	}
	owner := ownedPRInstance(t, f.paths.Data)
	saved, err := owner.readQuestions(a.PRID)
	if err != nil || len(saved.Threads) != 1 {
		t.Fatalf("shutdown history: %v", err)
	}
	last := saved.Threads[0].Answers[0]
	if saved.Version != before.Version || last.State != question.Pending || last.Text != "" {
		t.Fatal("shutdown saved an in-progress answer")
	}
}

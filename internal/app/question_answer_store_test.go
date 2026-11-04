package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/question"
)

// savedAnswer reads one exact answer from the actual private disk snapshot.
func savedAnswer(t *testing.T, f questionFixture, answer question.Answer) question.Answer {
	t.Helper()
	saved, err := f.owner.readQuestions(answer.PRID)
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range saved.Threads {
		for _, a := range thread.Answers {
			if a.ID == answer.ID {
				return a
			}
		}
	}
	t.Fatal("saved answer missing")
	return question.Answer{}
}

func TestAnswerSavesOnlyOnTerminalOutcomeAfterObserverDisconnect(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, a, true)
	beforeProgress, err := f.owner.readQuestions(a.PRID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	afterProgress, err := f.owner.readQuestions(a.PRID)
	if err != nil || afterProgress.Version != beforeProgress.Version {
		t.Fatal("streaming progress triggered a disk write")
	}
	if got := savedAnswer(t, f, a); got.State != question.Pending || got.Text != "" {
		t.Fatal("streaming text was saved before the response ended")
	}
	response := questionPost(t, context.Background(), s, answerPath(a)+"/stop", question.AnswerRequest{RevisionID: a.RevisionID})
	if response.StatusCode != 202 {
		t.Fatal("Stop failed")
	}
	terminal := observeHTTP(t, s, a, false)
	if terminal.State != question.Interrupted || terminal.Persistence != "saved" || terminal.ExecutionUnknown || terminal.SavedVersion == "" {
		t.Fatalf("terminal save: %+v", terminal)
	}
	onDisk := savedAnswer(t, f, a)
	if onDisk.Text != "Synthetic fixture " || onDisk.State != question.Interrupted || !onDisk.StopRequested || onDisk.Persistence != "" || onDisk.SavedVersion != "" {
		t.Fatal("terminal partial text/Stop was not saved")
	}
	before := len(nativeQuestionAudit(t, f.audit))
	retry := questionPost(t, context.Background(), s, answerPath(a)+"/save", question.AnswerRequest{RevisionID: a.RevisionID})
	if retry.StatusCode != 200 || len(nativeQuestionAudit(t, f.audit)) != before {
		t.Fatal("already saved retry invoked native activity")
	}
}

type terminalSaveRegistry struct {
	*admissionRegistry
	writing chan struct{}
	release chan struct{}
	once    sync.Once
}

// SaveQuestions gates a real terminal replacement to expose the save-before-slot-release boundary.
func (s *terminalSaveRegistry) SaveQuestions(next question.Saved, previous uint64) error {
	terminal := false
	for _, thread := range next.Threads {
		for _, answer := range thread.Answers {
			terminal = terminal || answer.State == question.Completed
		}
	}
	if terminal {
		s.once.Do(func() { close(s.writing); <-s.release })
	}
	return s.owner.SaveQuestions(next, previous)
}

func TestTerminalSaveKeepsRevisionBusyAndLateSavesCannotOverwriteFollowups(t *testing.T) {
	f := newQuestionFixture(t, "answer-delayed")
	registry := &terminalSaveRegistry{admissionRegistry: &admissionRegistry{owner: f.owner}, writing: make(chan struct{}), release: make(chan struct{})}
	r := question.New(registry, f.owner.cache)
	f.owner.questions = r
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(registry.release) }) })
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
	select {
	case <-registry.writing:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal write did not start")
	}
	current, err := r.ReadAnswer(a.PRID, a.ThreadID, a.ID, question.AnswerRequest{RevisionID: a.RevisionID})
	if err != nil || current.State != question.Completed || current.Persistence != "saving" {
		t.Fatalf("completion confused with saving: %v %+v", err, current)
	}
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); !errors.Is(err, question.ErrBusy) {
		t.Fatalf("slot released before save: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	release.Do(func() { close(registry.release) })
	terminal := observeHTTP(t, s, a, false)
	if terminal.Persistence != "saved" {
		t.Fatal("terminal saving acknowledgement missing")
	}
	next, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Only after final persistence"})
	if err != nil {
		t.Fatal(err)
	}
	response := questionPost(t, context.Background(), s, answerPath(a)+"/save", question.AnswerRequest{RevisionID: a.RevisionID})
	var late question.Answer
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&late) != nil || late.ID != a.ID {
		t.Fatal("late exact-answer save failed")
	}
	saved, err := f.owner.readQuestions(a.PRID)
	if err != nil || len(saved.Threads[0].Answers) != 2 || saved.Threads[0].Answers[1].ID != next.Snapshot().ID || saved.Threads[0].Answers[0].State != question.Completed {
		t.Fatal("late save overwrote a newer turn")
	}
	prompts := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "prompt" {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatal("storage-only retry replayed a prompt")
	}
}

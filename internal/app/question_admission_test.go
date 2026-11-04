package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/question"
)

func TestQuestionAdmissionIsSavedBeforeNativeDispatch(t *testing.T) {
	f := newQuestionFixture(t, "answer-admission-proof")
	t.Setenv("SHELDON_NATIVE_SAVED_ROOT", f.paths.Data)
	s := questionHTTP(t, f)
	first := askHTTP(t, s, f.pr.ID, f.input)
	if answer := observeHTTP(t, s, first, false); answer.State != question.Completed {
		t.Fatalf("native disk proof failed: %s", answer.State)
	}
	response := questionPost(t, context.Background(), s, "/api/reviews/"+f.pr.ID+"/threads/"+first.ThreadID+"/answers", question.FollowupRequest{RevisionID: first.RevisionID, Question: "Explicit follow-up"})
	if response.StatusCode != 202 {
		t.Fatal("follow-up was not admitted")
	}
	var followup question.Answer
	if err := json.NewDecoder(response.Body).Decode(&followup); err != nil {
		t.Fatal(err)
	}
	if answer := observeHTTP(t, s, followup, false); answer.State != question.Completed {
		t.Fatal("follow-up not saved before dispatch")
	}
	other := askHTTP(t, s, f.pr.ID, f.input)
	if answer := observeHTTP(t, s, other, false); answer.State != question.Completed {
		t.Fatal("new thread not saved before dispatch")
	}
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Bindings) != 1 || len(saved.Threads) != 2 || len(saved.Threads[0].Answers) != 2 {
		t.Fatalf("saved admissions: %v", err)
	}
	if saved.Threads[0].Answers[0].State != question.Completed || saved.Threads[0].Answers[1].State != question.Completed || saved.Threads[1].Answers[0].State != question.Completed {
		t.Fatal("terminal snapshots were not saved independently of later admissions")
	}
	proofs, prompts := 0, 0
	entries := nativeQuestionAudit(t, f.audit)
	for _, entry := range entries {
		if entry.AdmissionVerified {
			proofs++
		}
		if filepath.Base(entry.Path) == "prompt" {
			prompts++
		}
	}
	if proofs != 3 || prompts != 3 || countNativeSessions(entries) != 1 {
		t.Fatalf("proofs/prompts/sessions: %d/%d/%d", proofs, prompts, countNativeSessions(entries))
	}
}

func TestQuestionStoreFailureRejectsAdmissionBeforeNativeStartup(t *testing.T) {
	f := newQuestionFixture(t, "")
	path := filepath.Join(f.paths.Data, "reviews")
	if err := os.WriteFile(path, []byte("preserve foreign state"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
		if turn != nil || !errors.Is(err, question.ErrPersistence) {
			t.Fatalf("unsafe store admitted: %v", err)
		}
	}
	if len(nativeQuestionAudit(t, f.audit)) != 0 {
		t.Fatal("save failure started native service")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Threads) != 1 || saved.Threads[0].Answers[0].ID != turn.Snapshot().ID {
		t.Fatal("phantom history or unsaved admission")
	}
}

func TestSavedBindingPreventsReplacementAfterAppRestart(t *testing.T) {
	f := newQuestionFixture(t, "")
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	before := len(nativeQuestionAudit(t, f.audit))
	owner := ownedPRInstance(t, f.paths.Data)
	service := &sourceRegistry{instance: owner, paths: f.paths}
	if _, err := service.LoadSource(context.Background(), f.pr, httpapi.SourceRequest{Action: "load"}); err != nil {
		t.Fatal(err)
	}
	if turn, err := service.BeginQuestion(context.Background(), f.pr.ID, f.input); turn != nil || !errors.Is(err, question.ErrSessionUnavailable) {
		t.Fatalf("stored binding replaced: %v", err)
	}
	if len(nativeQuestionAudit(t, f.audit)) != before {
		t.Fatal("restart started native service or replacement session")
	}
	saved, err := owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Bindings) != 1 || len(saved.Threads) != 1 {
		t.Fatal("restart lost saved admission")
	}
}

func TestConcurrentRevisionAdmissionsPreserveSiblingRecords(t *testing.T) {
	f := newQuestionFixture(t, "")
	base := sourceGit(t, f.realGit, f.upstream, "another base\n", "commit-tree", f.tree, "-p", f.input.Revision.BaseSHA)
	other := prepareQuestionRevision(t, f, f.pr, base, f.head)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, input := range []question.InitialRequest{f.input, other} {
		wg.Go(func() {
			<-start
			if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, input); err != nil {
				t.Errorf("admission: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	saved, err := f.owner.readQuestions(f.pr.ID)
	if err != nil || len(saved.Bindings) != 2 || len(saved.Threads) != 2 || saved.Bindings[0].RevisionID == saved.Bindings[1].RevisionID {
		t.Fatalf("lost concurrent revision records: %v", err)
	}
	if countNativeSessions(nativeQuestionAudit(t, f.audit)) != 2 {
		t.Fatal("concurrent revision session identity mismatch")
	}
}

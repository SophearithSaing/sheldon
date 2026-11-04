package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// finishNativeTurn executes one admitted turn through the real runtime and synthetic native protocol.
func finishNativeTurn(t *testing.T, f questionFixture, turn *question.Turn) question.Answer {
	t.Helper()
	if err := turn.Start(); err != nil {
		t.Fatal(err)
	}
	a := turn.Snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var terminal question.Answer
	if err := f.service.ObserveAnswer(ctx, a.PRID, a.ThreadID, a.ID, question.AnswerRequest{RevisionID: a.RevisionID}, func(snapshot question.Answer) error { terminal = snapshot; return nil }); err != nil {
		t.Fatal(err)
	}
	if terminal.State != question.Completed || terminal.Persistence != "saved" {
		t.Fatalf("native terminal: %+v", terminal)
	}
	return terminal
}

// restartQuestionFixture reopens only the app writer; continuation must lazily attach existing source/native state.
func restartQuestionFixture(t *testing.T, f questionFixture) questionFixture {
	t.Helper()
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	f.owner = ownedPRInstance(t, f.paths.Data)
	f.service = &sourceRegistry{instance: f.owner, paths: f.paths}
	return f
}

func TestSavedThreadContinuationReusesExactSessionAcrossThreadsAndChangedDefaults(t *testing.T) {
	f := newQuestionFixture(t, "")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
	first, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	a := finishNativeTurn(t, f, first)
	second, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	b := finishNativeTurn(t, f, second)
	before := len(nativeQuestionAudit(t, f.audit))
	ghBefore, err := os.ReadFile(os.Getenv("SHELDON_TEST_GH_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	f = restartQuestionFixture(t, f)
	t.Setenv("SHELDON_NATIVE_MODE", "model-unavailable")
	turn, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "New explicit continuation after restart"})
	if err != nil {
		t.Fatalf("saved follow-up: %v", err)
	}
	if snapshot := turn.Snapshot(); snapshot.ThreadID != a.ThreadID || snapshot.RevisionID != a.RevisionID {
		t.Fatal("continuation retargeted original thread/revision")
	}
	if _, err := f.service.BeginFollowup(context.Background(), b.PRID, b.ThreadID, question.FollowupRequest{RevisionID: b.RevisionID, Question: "Concurrent sibling"}); !errors.Is(err, question.ErrBusy) {
		t.Fatalf("shared reopened session accepted concurrent work: %v", err)
	}
	for _, entry := range nativeQuestionAudit(t, f.audit)[before:] {
		if entry.Path == "/api/model/default" || entry.Path == "/api/session" || strings.HasSuffix(entry.Path, "/prompt") {
			t.Fatal("admission recreated a session, resolved defaults or dispatched before Start")
		}
	}
	finishNativeTurn(t, f, turn)
	sibling, err := f.service.BeginFollowup(context.Background(), b.PRID, b.ThreadID, question.FollowupRequest{RevisionID: b.RevisionID, Question: "Explicit sibling continuation"})
	if err != nil {
		t.Fatal(err)
	}
	finishNativeTurn(t, f, sibling)
	entries := nativeQuestionAudit(t, f.audit)
	prompts := 0
	for _, entry := range entries[before:] {
		if strings.HasSuffix(entry.Path, "/prompt") {
			prompts++
			if entry.Path != "/api/session/ses_fixture/prompt" {
				t.Fatal("saved threads did not share original native identity")
			}
		}
	}
	if prompts != 2 || countNativeSessions(entries) != 1 {
		t.Fatal("reopen duplicated native session or replayed saved input")
	}
	ghAfter, err := os.ReadFile(os.Getenv("SHELDON_TEST_GH_AUDIT"))
	if err != nil || string(ghBefore) != string(ghAfter) {
		t.Fatal("continuation loaded current GitHub metadata")
	}
	saved, err := f.owner.ReadQuestions(a.PRID)
	if err != nil || len(saved.Threads) != 2 || len(saved.Threads[0].Answers) != 2 || len(saved.Threads[1].Answers) != 2 {
		t.Fatal("continuation lost sibling history")
	}
	if saved.Bindings[0].Native.Model.ID != "chat" {
		t.Fatal("changed native default replaced saved model")
	}
	f = restartQuestionFixture(t, f)
	third, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Continue again without replay"})
	if err != nil {
		t.Fatalf("second native reopen: %v", err)
	}
	finishNativeTurn(t, f, third)
}

func TestSavedThreadContinuationFailsClosedWithoutReplacement(t *testing.T) {
	for _, control := range []string{"missing-session", "changed-session", "history-empty", "history-busy", "inbox-busy"} {
		t.Run(control, func(t *testing.T) {
			f := newQuestionFixture(t, "")
			t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
			turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
			if err != nil {
				t.Fatal(err)
			}
			a := finishNativeTurn(t, f, turn)
			f = restartQuestionFixture(t, f)
			before := len(nativeQuestionAudit(t, f.audit))
			if err := os.WriteFile(f.control, []byte(control), 0600); err != nil {
				t.Fatal(err)
			}
			if turn, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Do not replace the original"}); turn != nil || !errors.Is(err, question.ErrSessionUnavailable) {
				t.Fatalf("unsafe continuation: %v", err)
			}
			for _, entry := range nativeQuestionAudit(t, f.audit)[before:] {
				if entry.Path == "/api/model/default" || entry.Path == "/api/session" || strings.HasSuffix(entry.Path, "/prompt") {
					t.Fatal("failed continuation created native state or replayed input")
				}
			}
			saved, err := f.owner.ReadQuestions(a.PRID)
			if err != nil || len(saved.Threads[0].Answers) != 1 || saved.Threads[0].Answers[0].Text != a.Text {
				t.Fatal("failed continuation lost readable history or admitted a phantom turn")
			}
		})
	}
}

func TestSavedContinuationDoesNotRepairMissingSource(t *testing.T) {
	f := newQuestionFixture(t, "")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	a := finishNativeTurn(t, f, turn)
	f = restartQuestionFixture(t, f)
	if err := os.Rename(f.paths.Cache, f.paths.Cache+"-removed"); err != nil {
		t.Fatal(err)
	}
	before := len(nativeQuestionAudit(t, f.audit))
	if _, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Missing original source"}); !errors.Is(err, git.ErrQuestionSourceUnavailable) {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := os.Stat(f.paths.Cache); !os.IsNotExist(err) {
		t.Fatal("continuation recreated missing cache")
	}
	if len(nativeQuestionAudit(t, f.audit)) != before {
		t.Fatal("missing source started native service")
	}
	saved, err := f.owner.ReadQuestions(a.PRID)
	if err != nil || saved.Threads[0].Answers[0].Text != a.Text {
		t.Fatal("cache loss destroyed saved transcript")
	}
}

func TestSavedContinuationKeepsPRAndBaseHeadRevisionBindingsSeparate(t *testing.T) {
	f := newQuestionFixture(t, "")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
	other, err := f.owner.RegisterPR("https://github.com/owner/repo/pull/32")
	if err != nil {
		t.Fatal(err)
	}
	newBase := sourceGit(t, f.realGit, f.upstream, "another base\n", "commit-tree", f.tree, "-p", f.input.Revision.BaseSHA)
	newHead := sourceGit(t, f.realGit, f.upstream, "another head\n", "commit-tree", f.tree, "-p", f.head)
	inputs := []question.InitialRequest{f.input, prepareQuestionRevision(t, f, f.pr, newBase, f.head), prepareQuestionRevision(t, f, f.pr, newBase, newHead), prepareQuestionRevision(t, f, other, f.input.Revision.BaseSHA, f.head)}
	answers := []question.Answer{}
	for _, input := range inputs {
		turn, err := f.service.BeginQuestion(context.Background(), input.Revision.PRID, input)
		if err != nil {
			t.Fatal(err)
		}
		answers = append(answers, finishNativeTurn(t, f, turn))
	}
	originalPaths := []string{}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/prompt") {
			originalPaths = append(originalPaths, entry.Path)
		}
	}
	f = restartQuestionFixture(t, f)
	before := len(nativeQuestionAudit(t, f.audit))
	for _, answer := range answers {
		turn, err := f.service.BeginFollowup(context.Background(), answer.PRID, answer.ThreadID, question.FollowupRequest{RevisionID: answer.RevisionID, Question: "Continue only the original revision"})
		if err != nil {
			t.Fatalf("isolated saved revision: %v", err)
		}
		finishNativeTurn(t, f, turn)
	}
	n := 0
	for _, entry := range nativeQuestionAudit(t, f.audit)[before:] {
		if entry.Path == "/api/session" || entry.Path == "/api/model/default" {
			t.Fatal("isolated continuation recreated a binding")
		}
		if strings.HasSuffix(entry.Path, "/prompt") {
			if n >= len(originalPaths) || entry.Path != originalPaths[n] {
				t.Fatal("PR/base/head bindings were shared or retargeted")
			}
			var input struct {
				Text string `json:"text"`
			}
			if json.Unmarshal([]byte(entry.Body), &input) != nil || !strings.Contains(input.Text, answers[n].RevisionID) {
				t.Fatal("prompt substituted a newer revision")
			}
			n++
		}
	}
	if n != 4 {
		t.Fatal("missing exact-revision continuations")
	}
}

func TestIncompatibleSavedBindingKeepsHistoryReadable(t *testing.T) {
	f := newQuestionFixture(t, "")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native"))
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	a := finishNativeTurn(t, f, turn)
	path := filepath.Join(f.paths.Data, "reviews", a.PRID, "threads.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"version", "protocol", "policy", "variant"} {
		f = restartQuestionFixture(t, f)
		var saved question.Saved
		if err := json.Unmarshal(original, &saved); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "version":
			saved.Bindings[0].Native.Version = "2.2.0"
		case "protocol":
			saved.Bindings[0].Native.Protocol = "unsupported-protocol"
		case "policy":
			saved.Bindings[0].Native.Policy++
		case "variant":
			saved.Bindings[0].Native.Model.Variant = "unsupported"
		}
		data, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		before := len(nativeQuestionAudit(t, f.audit))
		history, err := f.owner.ReadQuestions(a.PRID)
		if err != nil || history.Threads[0].Answers[0].Text != a.Text {
			t.Fatalf("incompatible %s hid saved history: %v", kind, err)
		}
		if turn, err := f.service.BeginFollowup(context.Background(), a.PRID, a.ThreadID, question.FollowupRequest{RevisionID: a.RevisionID, Question: "Do not replace incompatible state"}); turn != nil || !errors.Is(err, question.ErrSessionUnavailable) {
			t.Fatalf("incompatible %s resumed: %v", kind, err)
		}
		if len(nativeQuestionAudit(t, f.audit)) != before {
			t.Fatal("incompatible binding started native service")
		}
	}
}

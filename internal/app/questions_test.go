package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/platform"
	"github.com/SophearithSaing/sheldon/internal/question"
)

type questionFixture struct {
	paths                      platform.Paths
	owner                      *instance
	service                    *sourceRegistry
	pr                         githubpr.PullRequest
	input                      question.InitialRequest
	realGit, upstream, tree    string
	audit, control, base, head string
}

// newQuestionFixture composes private app storage, real Git objects and fake native services offline.
func newQuestionFixture(t *testing.T, mode string) questionFixture {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"git": "../git/testdata/git", "gh": "../github/testdata/gh", "opencode": "../opencode/testdata/opencode"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(bin, name), path)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture build: %v %s", err, data)
		}
	}
	upstream := filepath.Join(root, "upstream.git")
	sourceGit(t, realGit, root, "", "init", "--bare", upstream)
	empty := sourceGit(t, realGit, upstream, "", "mktree")
	oldBlob := sourceGit(t, realGit, upstream, "old literal\n", "hash-object", "-w", "--stdin")
	oldTree := sourceGit(t, realGit, upstream, "100644 blob "+oldBlob+"\tsource.txt\n", "mktree")
	base := sourceGit(t, realGit, upstream, "base\n", "commit-tree", oldTree)
	newBlob := sourceGit(t, realGit, upstream, "new literal\n", "hash-object", "-w", "--stdin")
	tree := sourceGit(t, realGit, upstream, "100644 blob "+newBlob+"\tsource.txt\n", "mktree")
	head := sourceGit(t, realGit, upstream, "head\n", "commit-tree", tree, "-p", base)
	baseTip := sourceGit(t, realGit, upstream, "advanced base\n", "commit-tree", empty, "-p", base)
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", baseTip)
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/pull/31/head", head)
	body, err := os.ReadFile("../github/testdata/pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	response := strings.ReplaceAll(strings.ReplaceAll(string(body), strings.Repeat("a", 40), baseTip), strings.Repeat("b", 40), head)
	audit, control := filepath.Join(root, "native-audit.jsonl"), filepath.Join(root, "native-control")
	for name, value := range map[string]string{
		"PATH": bin, "SHELDON_GIT_REAL": realGit, "SHELDON_GIT_UPSTREAM": upstream, "SHELDON_GIT_AUDIT": filepath.Join(root, "git-audit"), "SHELDON_GIT_MODE": "",
		"SHELDON_TEST_GH_RESPONSE": "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n" + response, "SHELDON_TEST_GH_MODE": "",
		"SHELDON_TEST_GH_AUDIT": filepath.Join(root, "gh-audit"), "GH_TOKEN": "synthetic-auth-token", "GIT_SSH_COMMAND": "preserved-fixture-ssh-route",
		"SHELDON_NATIVE_MODE": mode, "SHELDON_NATIVE_AUDIT": audit, "SHELDON_NATIVE_CONTROL": control, "SHELDON_SYNTHETIC_AUTH": "fixture-route",
		"OPENCODE_CONFIG": "", "OPENCODE_CONFIG_DIR": "", "OPENCODE_CONFIG_CONTENT": "", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(name, value)
	}
	paths := testPaths(t)
	owner, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	})
	if owned, err := owner.acquire(); err != nil || !owned {
		t.Fatal("writer unavailable")
	}
	pr, err := owner.RegisterPR("https://github.com/owner/repo/pull/31")
	if err != nil {
		t.Fatal(err)
	}
	service := &sourceRegistry{instance: owner, paths: paths}
	ready, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "load"})
	if err != nil || ready.Diff == nil {
		t.Fatalf("native source fixture: %v", err)
	}
	input := question.InitialRequest{Revision: ready.Diff.Revision, Question: "Explain the selected change.", Anchor: question.Anchor{Kind: "lines", FileID: ready.Diff.Files[0].ID, HunkID: ready.Diff.Files[0].Hunks[0].ID, Side: "new", Start: 1, End: 1}}
	return questionFixture{paths: paths, owner: owner, service: service, pr: pr, input: input, realGit: realGit, upstream: upstream, tree: tree, audit: audit, control: control, base: base, head: head}
}

type questionAuditEntry struct {
	Args      []string `json:"args"`
	Directory string   `json:"directory"`
	Method    string   `json:"method"`
	Path      string   `json:"path"`
	Body      string   `json:"body"`
}

// nativeQuestionAudit reads only test-owned synthetic native metadata, never real credentials.
func nativeQuestionAudit(t *testing.T, path string) []questionAuditEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var result []questionAuditEntry
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var entry questionAuditEntry
		if json.Unmarshal(line, &entry) != nil {
			t.Fatal("malformed synthetic native audit")
		}
		result = append(result, entry)
	}
	return result
}

// countNativeSessions counts actual native fixture admission, not question UI counts.
func countNativeSessions(entries []questionAuditEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Method == "POST" && entry.Path == "/api/session" {
			count++
		}
	}
	return count
}

func TestQuestionRuntimeSharesNativeContextWithSeparateThreads(t *testing.T) {
	f := newQuestionFixture(t, "")
	r, err := f.owner.openQuestions(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(nativeQuestionAudit(t, f.audit)) != 0 {
		t.Fatal("opening runtime or source started a harness")
	}
	first, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); !errors.Is(err, question.ErrBusy) {
		t.Fatal("second question was queued or admitted while busy")
	}
	one := first.Snapshot()
	if _, err := f.service.BeginFollowup(context.Background(), f.pr.ID, one.ThreadID, question.FollowupRequest{RevisionID: one.RevisionID, Question: "While busy?"}); !errors.Is(err, question.ErrBusy) {
		t.Fatal("follow-up bypassed shared busy state")
	}
	if err := first.Finish(question.Completed, "Synthetic first answer; not model inference."); err != nil {
		t.Fatal(err)
	}
	input := f.input
	input.Question = "A separate UI question."
	second, err := f.service.BeginQuestion(context.Background(), f.pr.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	two := second.Snapshot()
	if two.ThreadID == one.ThreadID || two.ID == one.ID || strings.Contains(second.Prompt(), "Synthetic first answer") {
		t.Fatal("separate UI threads copied transcript or answer identities")
	}
	if err := first.Finish(question.Completed, "Synthetic first answer; not model inference."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, input); !errors.Is(err, question.ErrBusy) {
		t.Fatal("late completion released another thread's active slot")
	}
	second.Finish(question.Failed, "Synthetic partial failure.")
	followup, err := f.service.BeginFollowup(context.Background(), f.pr.ID, one.ThreadID, question.FollowupRequest{RevisionID: one.RevisionID, Question: "Explain the old behavior."})
	if err != nil || !strings.Contains(followup.Prompt(), "Synthetic first answer") || strings.Contains(followup.Prompt(), "A separate UI question") || strings.Contains(followup.Prompt(), f.paths.Cache) {
		t.Fatalf("thread-local follow-up context: %v", err)
	}
	if followup.Snapshot().ThreadID != one.ThreadID || followup.Snapshot().ID == one.ID {
		t.Fatal("follow-up did not retain its submitting thread")
	}
	entries := nativeQuestionAudit(t, f.audit)
	if countNativeSessions(entries) != 1 {
		t.Fatal("new threads/follow-ups created independent native histories")
	}
	for _, entry := range entries {
		if strings.Contains(entry.Path, "/prompt") || strings.Contains(entry.Path, "/connect") {
			t.Fatal("session binding dispatched inference or authentication")
		}
	}
	got, err := r.ReadAnswer(f.pr.ID, one.ThreadID, one.ID, question.AnswerRequest{RevisionID: one.RevisionID})
	if err != nil || got.State != question.Completed || second.Snapshot().State != question.Failed || followup.Snapshot().State != question.Pending {
		t.Fatal("answer routing replaced another thread's transcript")
	}
	if err := followup.Uncertain(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); !errors.Is(err, question.ErrUncertain) {
		t.Fatal("uncertain execution allowed a new question in the shared session")
	}
	followup.Finish(question.Interrupted, "Synthetic interrupted turn.")
}

func TestConcurrentQuestionAdmissionCreatesOneNativeSession(t *testing.T) {
	f := newQuestionFixture(t, "")
	start := make(chan struct{})
	var wg sync.WaitGroup
	turns := make(chan *question.Turn, 16)
	failures := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
			if err != nil {
				failures <- err
			} else {
				turns <- turn
			}
		}()
	}
	close(start)
	wg.Wait()
	close(turns)
	close(failures)
	if len(turns) != 1 || len(failures) != 15 || countNativeSessions(nativeQuestionAudit(t, f.audit)) != 1 {
		t.Fatal("simultaneous tabs created competing native sessions or admitted overlapping work")
	}
	for err := range failures {
		if !errors.Is(err, question.ErrBusy) {
			t.Fatalf("unexpected concurrent admission failure: %v", err)
		}
	}
	for turn := range turns {
		turn.Finish(question.Completed, "Synthetic fixture result.")
	}
}

// prepareQuestionRevision creates verified source for a separate offline revision/PR.
func prepareQuestionRevision(t *testing.T, f questionFixture, pr githubpr.PullRequest, base, head string) question.InitialRequest {
	t.Helper()
	sourceGit(t, f.realGit, f.upstream, "", "update-ref", "refs/heads/feat/go-rewrite", base)
	sourceGit(t, f.realGit, f.upstream, "", "update-ref", fmt.Sprintf("refs/pull/%d/head", pr.Number), head)
	resolution := github.Resolution{PR: pr, State: github.Resolved, Base: &github.Base{Branch: "feat/go-rewrite", SHA: base, Source: githubpr.GitHubBase}, HeadSHA: head}
	snapshot, err := f.owner.cache.Fetch(context.Background(), resolution)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := f.owner.cache.Prepare(context.Background(), pr, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := f.owner.cache.Diff(context.Background(), pr, prepared.Revision)
	if err != nil || len(diff.Files) != 1 {
		t.Fatalf("question refresh fixture: %v", err)
	}
	input := f.input
	input.Revision, input.Anchor.FileID, input.Anchor.HunkID = prepared.Revision, diff.Files[0].ID, diff.Files[0].Hunks[0].ID
	return input
}

func TestQuestionBindingsSeparatePRsAndRevisionsIncludingSharedHead(t *testing.T) {
	f := newQuestionFixture(t, "default-changes")
	original, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	newBase := sourceGit(t, f.realGit, f.upstream, "another base\n", "commit-tree", f.tree, "-p", f.input.Revision.BaseSHA)
	newInput := prepareQuestionRevision(t, f, f.pr, newBase, f.head)
	newer, err := f.service.BeginQuestion(context.Background(), f.pr.ID, newInput)
	if err != nil || newer.Snapshot().RevisionID == original.Snapshot().RevisionID {
		t.Fatalf("independent base-only revision: %v", err)
	}
	otherPR, err := f.owner.RegisterPR("https://github.com/owner/repo/pull/32")
	if err != nil {
		t.Fatal(err)
	}
	otherInput := prepareQuestionRevision(t, f, otherPR, newBase, f.head)
	other, err := f.service.BeginQuestion(context.Background(), otherPR.ID, otherInput)
	if err != nil {
		t.Fatal(err)
	}
	newHead := sourceGit(t, f.realGit, f.upstream, "new pinned head\n", "commit-tree", f.tree, "-p", f.base)
	headInput := prepareQuestionRevision(t, f, f.pr, newBase, newHead)
	headTurn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, headInput)
	if err != nil || headTurn.Snapshot().RevisionID == newer.Snapshot().RevisionID {
		t.Fatalf("independent head-only revision: %v", err)
	}
	original.Finish(question.Completed, "Synthetic original-revision answer.")
	old, err := f.service.BeginFollowup(context.Background(), f.pr.ID, original.Snapshot().ThreadID, question.FollowupRequest{RevisionID: original.Snapshot().RevisionID, Question: "Original context?"})
	if err != nil || old.Snapshot().RevisionID != original.Snapshot().RevisionID || !strings.Contains(old.Prompt(), f.input.Revision.BaseSHA) {
		t.Fatalf("older thread rebound after refresh: %v", err)
	}
	entries := nativeQuestionAudit(t, f.audit)
	if countNativeSessions(entries) != 4 {
		t.Fatal("different PRs/revisions shared a native question session")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, entry := range entries {
		if entry.Method == "GET" && strings.HasPrefix(entry.Path, "/api/session/") {
			seen[entry.Path] = true
		}
		if entry.Path == "/api/session" {
			var body struct {
				Model struct {
					ID string `json:"id"`
				} `json:"model"`
			}
			json.Unmarshal([]byte(entry.Body), &body)
			models = append(models, body.Model.ID)
		}
	}
	if len(seen) != 4 || len(models) != 4 || models[0] != "chat" || models[1] != "changed-default" {
		t.Fatal("bindings lost exact native identity or new-work-only defaults")
	}
	if sourceGit(t, f.realGit, filepath.Join(f.paths.Cache, "worktrees", f.pr.ID, f.head), "", "rev-parse", "HEAD") != f.head || strings.Contains(old.Prompt(), newHead) {
		t.Fatal("head refresh rebound the original session/source")
	}
	old.Finish(question.Interrupted, "Synthetic original turn interrupted.")
	newer.Finish(question.Completed, "Synthetic new revision answer.")
	other.Finish(question.Completed, "Synthetic other PR answer.")
	headTurn.Finish(question.Completed, "Synthetic changed-head answer.")
}

func TestQuestionFollowupRefusesMissingSourceOrNativeState(t *testing.T) {
	for _, failure := range []string{"dirty source", "missing-session", "changed-session"} {
		t.Run(failure, func(t *testing.T) {
			f := newQuestionFixture(t, "")
			turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
			if err != nil {
				t.Fatal(err)
			}
			turn.Finish(question.Completed, "Synthetic retained answer.")
			if failure == "dirty source" {
				path := filepath.Join(f.paths.Cache, "worktrees", f.pr.ID, f.head, "source.txt")
				if err := os.WriteFile(path, []byte("keep user edits"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(f.control, []byte(failure), 0600); err != nil {
				t.Fatal(err)
			}
			followup := question.FollowupRequest{RevisionID: turn.Snapshot().RevisionID, Question: "Continue original thread."}
			_, err = f.service.BeginFollowup(context.Background(), f.pr.ID, turn.Snapshot().ThreadID, followup)
			if err == nil || failure == "dirty source" && !errors.Is(err, git.ErrQuestionSourceUnavailable) || failure != "dirty source" && !errors.Is(err, question.ErrSessionUnavailable) {
				t.Fatalf("missing original state accepted: %v", err)
			}
			if countNativeSessions(nativeQuestionAudit(t, f.audit)) != 1 || turn.Snapshot().Text != "Synthetic retained answer." {
				t.Fatal("failed follow-up created a replacement or erased history")
			}
		})
	}
}

func TestQuestionStartupFailureIsNotAutomaticallyRetried(t *testing.T) {
	f := newQuestionFixture(t, "version-wrong")
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); err == nil {
		t.Fatal("unsupported harness admitted a question")
	}
	t.Setenv("SHELDON_NATIVE_MODE", "")
	if _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); err == nil {
		t.Fatal("failed native service was silently replaced")
	}
	if len(nativeQuestionAudit(t, f.audit)) != 1 {
		t.Fatal("startup was retried automatically")
	}
}

func TestQuestionShutdownCancelsPreparationBeforeReleasingWriter(t *testing.T) {
	f := newQuestionFixture(t, "create-hang")
	done := make(chan error, 1)
	go func() { _, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input); done <- err }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waiting := true
	for waiting {
		select {
		case err := <-done:
			t.Fatalf("fixture preparation ended before shutdown: %v", err)
		case <-deadline.C:
			t.Fatal("native session fixture did not enter creation")
		case <-ticker.C:
			if _, err := os.Stat(f.control + ".started"); err == nil {
				waiting = false
			}
		}
	}
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown admitted unfinished work")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer shutdown deadlocked with question preparation")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if len(entry.Args) > 0 {
			if _, err := os.Stat(entry.Directory); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("owned native startup state survived writer close")
			}
		}
	}
	contender, err := openInstance(f.paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer contender.close()
	if owned, err := contender.acquire(); err != nil || !owned {
		t.Fatal("writer was not released after native cleanup")
	}
}

func TestQuestionAdmissionDoesNotInitializeMissingSourceCache(t *testing.T) {
	paths := testPaths(t)
	owner, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if owned, err := owner.acquire(); err != nil || !owned {
		t.Fatal("writer unavailable")
	}
	pr, err := owner.RegisterPR("https://github.com/owner/repo/pull/31")
	if err != nil {
		t.Fatal(err)
	}
	input := question.InitialRequest{Revision: githubpr.Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}, Anchor: question.Anchor{Kind: "hunk", FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64)}, Question: "Source must already exist."}
	service := &sourceRegistry{instance: owner, paths: paths}
	if _, err := service.BeginQuestion(context.Background(), pr.ID, input); !errors.Is(err, git.ErrQuestionSourceUnavailable) {
		t.Fatalf("missing original cache accepted: %v", err)
	}
	if owner.cache != nil || owner.questions != nil {
		t.Fatal("question initialized cache/runtime without prepared source")
	}
	if _, err := os.Stat(paths.Cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("question recreated the missing cache root")
	}
	if _, err := service.BeginFollowup(context.Background(), pr.ID, strings.Repeat("e", 64), question.FollowupRequest{RevisionID: strings.Repeat("f", 64), Question: "Unknown thread."}); !errors.Is(err, question.ErrSessionUnavailable) {
		t.Fatal("unknown follow-up initialized runtime state")
	}
}

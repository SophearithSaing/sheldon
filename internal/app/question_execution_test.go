package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// questionHTTP exposes actual app question operations through the protected production HTTP layer.
func questionHTTP(t *testing.T, f questionFixture) *httpapi.Server {
	t.Helper()
	s, err := httpapi.Listen("", "", f.service)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Serve(); close(done) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.Shutdown(ctx)
		<-done
	})
	return s
}

// questionPost sends fixture credentials only in explicit bearer headers, never request URLs.
func questionPost(t *testing.T, ctx context.Context, s *httpapi.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequestWithContext(ctx, "POST", s.URL()+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal("fixture HTTP request failed")
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

// askHTTP decodes a real admission response without treating it as native completion.
func askHTTP(t *testing.T, s *httpapi.Server, prID string, input question.InitialRequest) question.Answer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := questionPost(t, ctx, s, "/api/reviews/"+prID+"/questions", input)
	if r.StatusCode != 202 {
		data, _ := io.ReadAll(r.Body)
		t.Fatalf("question admission: %d %s", r.StatusCode, data)
	}
	var answer question.Answer
	if json.NewDecoder(r.Body).Decode(&answer) != nil || answer.State != question.Pending {
		t.Fatal("admission falsely claimed completion")
	}
	return answer
}

// answerPath constructs a credential-free routed answer identity.
func answerPath(answer question.Answer) string {
	return "/api/reviews/" + answer.PRID + "/threads/" + answer.ThreadID + "/answers/" + answer.ID
}

// observeHTTP reads normalized cumulative snapshots; cancellation affects this observer only.
func observeHTTP(t *testing.T, s *httpapi.Server, answer question.Answer, untilStreaming bool) question.Answer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := questionPost(t, ctx, s, answerPath(answer)+"/events", question.AnswerRequest{RevisionID: answer.RevisionID})
	if r.StatusCode != 200 {
		data, _ := io.ReadAll(r.Body)
		t.Fatalf("answer observation: %d %s", r.StatusCode, data)
	}
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	last := question.Answer{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &last) != nil {
			t.Fatal("invalid public SSE JSON")
		}
		if last.ID != answer.ID || last.ThreadID != answer.ThreadID || last.RevisionID != answer.RevisionID {
			t.Fatal("stream crossed submitting scope")
		}
		for _, secret := range []string{"RAW PROVIDER", "HIDDEN REASONING", "RAW TOOL", "FOREIGN TEXT"} {
			if strings.Contains(line, secret) {
				t.Fatal("private native output leaked")
			}
		}
		if strings.Contains(line, "ses_fixture") || strings.Contains(line, "sheldon-opencode-") {
			t.Fatal("native identities/paths leaked")
		}
		if untilStreaming && last.State == question.Streaming {
			cancel()
			return last
		}
	}
	if scanner.Err() != nil {
		t.Fatal("fixture answer stream did not terminate")
	}
	return last
}

func TestHTTPQuestionsStreamWithoutRedispatchAfterBrowserDisconnect(t *testing.T) {
	f := newQuestionFixture(t, "answer-delayed")
	s := questionHTTP(t, f)
	answer := askHTTP(t, s, f.pr.ID, f.input)
	partial := observeHTTP(t, s, answer, true)
	if partial.Text == "" {
		t.Fatal("no visible native fixture delta streamed")
	}
	complete := observeHTTP(t, s, answer, false)
	if complete.State != question.Completed || complete.Text != "Synthetic fixture answer <script>literal</script>." || complete.ExecutionUnknown {
		t.Fatalf("completion: %+v", complete)
	}
	if complete.Reference.Anchor != f.input.Anchor || complete.Reference.RevisionID != answer.RevisionID {
		t.Fatal("reference was not backend-validated pinned selection")
	}
	prompts := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/prompt") {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatal("observer reconnect resubmitted a prompt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := questionPost(t, ctx, s, answerPath(answer)+"/stop", question.AnswerRequest{RevisionID: answer.RevisionID})
	if r.StatusCode != 202 {
		t.Fatal("late Stop failed")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/interrupt") {
			t.Fatal("late Stop interrupted a finished session")
		}
	}
}

func TestHTTPStopTargetsOnlySubmittingAnswerAndKeepsFollowupSession(t *testing.T) {
	f := newQuestionFixture(t, "answer-target-stop")
	s := questionHTTP(t, f)
	first := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, first, true)
	otherPR, err := f.owner.RegisterPR("https://github.com/owner/repo/pull/32")
	if err != nil {
		t.Fatal(err)
	}
	otherInput := prepareQuestionRevision(t, f, otherPR, f.input.Revision.BaseSHA, f.head)
	other := askHTTP(t, s, otherPR.ID, otherInput)
	if terminal := observeHTTP(t, s, other, false); terminal.State != question.Completed {
		t.Fatal("independent session did not complete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, scope := range []struct {
		path, revision string
	}{
		{answerPath(first), strings.Repeat("a", 64)},
		{"/api/reviews/" + other.PRID + "/threads/" + first.ThreadID + "/answers/" + first.ID, first.RevisionID},
		{"/api/reviews/" + first.PRID + "/threads/" + other.ThreadID + "/answers/" + first.ID, first.RevisionID},
	} {
		for _, action := range []string{"events", "stop"} {
			r := questionPost(t, ctx, s, scope.path+"/"+action, question.AnswerRequest{RevisionID: scope.revision})
			if r.StatusCode != 404 {
				data, _ := io.ReadAll(r.Body)
				t.Fatalf("wrong scope %s/%s returned %d: %s", scope.path, action, r.StatusCode, data)
			}
		}
	}
	r := questionPost(t, ctx, s, answerPath(first)+"/stop", question.AnswerRequest{RevisionID: first.RevisionID})
	if r.StatusCode != 202 {
		t.Fatal("targeted Stop request failed")
	}
	stopped := observeHTTP(t, s, first, false)
	if stopped.State != question.Interrupted || !stopped.StopRequested || stopped.ExecutionUnknown || stopped.Text == "" {
		t.Fatalf("confirmed interruption: %+v", stopped)
	}
	followPath := "/api/reviews/" + first.PRID + "/threads/" + first.ThreadID + "/answers"
	r = questionPost(t, ctx, s, followPath, question.FollowupRequest{RevisionID: first.RevisionID, Question: "Follow up on stopped fixture."})
	var follow question.Answer
	if r.StatusCode != 202 || json.NewDecoder(r.Body).Decode(&follow) != nil {
		t.Fatal("follow-up admission failed")
	}
	observeHTTP(t, s, follow, true)
	before := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/interrupt") {
			before++
		}
	}
	questionPost(t, ctx, s, answerPath(first)+"/stop", question.AnswerRequest{RevisionID: first.RevisionID})
	after := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/interrupt") {
			after++
			if !strings.Contains(entry.Path, "ses_fixture/interrupt") {
				t.Fatal("Stop reached another native session")
			}
		}
	}
	if after != before || countNativeSessions(nativeQuestionAudit(t, f.audit)) != 2 {
		t.Fatal("late Stop affected a follow-up or follow-up reset its session")
	}
	questionPost(t, ctx, s, answerPath(follow)+"/stop", question.AnswerRequest{RevisionID: follow.RevisionID})
	if terminal := observeHTTP(t, s, follow, false); terminal.State != question.Interrupted {
		t.Fatal("follow-up did not stop")
	}
}

func TestHTTPAmbiguousAdmissionAndFeedLossReconcileWithoutPromptReplay(t *testing.T) {
	for _, mode := range []string{"answer-ambiguous", "answer-feed-loss", "answer-ambiguous-empty"} {
		t.Run(mode, func(t *testing.T) {
			f := newQuestionFixture(t, mode)
			s := questionHTTP(t, f)
			answer := askHTTP(t, s, f.pr.ID, f.input)
			terminal := observeHTTP(t, s, answer, false)
			if mode == "answer-ambiguous-empty" {
				if terminal.State != question.Interrupted || !terminal.ExecutionUnknown {
					t.Fatal("uncertain dispatch was reported complete/idle")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				r := questionPost(t, ctx, s, "/api/reviews/"+f.pr.ID+"/questions", f.input)
				if r.StatusCode != 409 {
					t.Fatal("uncertain session admitted new work")
				}
			} else if terminal.State != question.Completed || terminal.Text != "Synthetic fixture answer <script>literal</script>." {
				t.Fatalf("history recovery: %+v", terminal)
			}
			prompts := 0
			for _, entry := range nativeQuestionAudit(t, f.audit) {
				if strings.HasSuffix(entry.Path, "/prompt") {
					prompts++
				}
			}
			if prompts != 1 {
				t.Fatal("reconciliation redispatched native input")
			}
		})
	}
}

func TestExecutionRevalidatesSourceAndShutdownDrainsActiveAnswer(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.paths.Cache, "worktrees", f.pr.ID, f.head, "source.txt")
	if err := os.WriteFile(path, []byte("keep edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := turn.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	last := question.Answer{}
	if err := f.service.ObserveAnswer(ctx, f.pr.ID, turn.Snapshot().ThreadID, turn.Snapshot().ID, question.AnswerRequest{RevisionID: turn.Snapshot().RevisionID}, func(a question.Answer) error { last = a; return nil }); err != nil {
		t.Fatal(err)
	}
	if last.State != question.Failed || last.FailureCode != "source-unavailable" {
		t.Fatal("source changed between admission/dispatch was accepted")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/prompt") {
			t.Fatal("dirty source dispatched native input")
		}
	}
	if err := os.WriteFile(path, []byte("new literal\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := questionHTTP(t, f)
	answer := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, answer, true)
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if len(entry.Args) > 0 {
			if _, err := os.Stat(entry.Directory); !os.IsNotExist(err) {
				t.Fatal("active answer left owned native startup state")
			}
		}
	}
}

func TestRepeatedStartAndSlowObserverDoNotBlockExecution(t *testing.T) {
	f := newQuestionFixture(t, "answer-delayed")
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		first := true
		done <- f.service.ObserveAnswer(ctx, f.pr.ID, turn.Snapshot().ThreadID, turn.Snapshot().ID, question.AnswerRequest{RevisionID: turn.Snapshot().RevisionID}, func(question.Answer) error {
			if first {
				first = false
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}()
	<-entered
	for range 3 {
		if err := turn.Start(); err != nil {
			t.Fatal(err)
		}
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for turn.Snapshot().State != question.Completed {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("slow observer blocked native completion")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	prompts := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if strings.HasSuffix(entry.Path, "/prompt") {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatal("repeated Start dispatched duplicate prompts")
	}
}

func TestConcurrentHTTPClientsShareOneActiveRevisionSession(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	s := questionHTTP(t, f)
	body, err := json.Marshal(f.input)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	admitted := make(chan question.Answer, 2)
	var work sync.WaitGroup
	for range 2 {
		work.Add(1)
		go func() {
			defer work.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, err := http.NewRequestWithContext(ctx, "POST", s.URL()+"/api/reviews/"+f.pr.ID+"/questions", bytes.NewReader(body))
			if err != nil {
				statuses <- 0
				return
			}
			r.Header.Set("Authorization", "Bearer "+s.APIToken())
			r.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(r)
			if err != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
			if response.StatusCode == 202 {
				var answer question.Answer
				if json.NewDecoder(response.Body).Decode(&answer) == nil {
					admitted <- answer
				}
			}
		}()
	}
	close(start)
	work.Wait()
	one, two := <-statuses, <-statuses
	if !((one == 202 && two == 409) || (one == 409 && two == 202)) || len(admitted) != 1 {
		t.Fatal("concurrent HTTP tabs bypassed shared admission")
	}
	answer := <-admitted
	observeHTTP(t, s, answer, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	questionPost(t, ctx, s, answerPath(answer)+"/stop", question.AnswerRequest{RevisionID: answer.RevisionID})
	if stopped := observeHTTP(t, s, answer, false); stopped.State != question.Interrupted || stopped.ExecutionUnknown {
		t.Fatal("owned concurrent answer did not confirm interruption")
	}
	if countNativeSessions(nativeQuestionAudit(t, f.audit)) != 1 {
		t.Fatal("concurrent HTTP tabs created separate native histories")
	}
}

func TestHTTPRefreshKeepsOldThreadOnItsOriginalNativeSession(t *testing.T) {
	f := newQuestionFixture(t, "answer-delayed")
	s := questionHTTP(t, f)
	original := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, original, false)
	newBase := sourceGit(t, f.realGit, f.upstream, "base advanced\n", "commit-tree", f.tree, "-p", f.input.Revision.BaseSHA)
	baseInput := prepareQuestionRevision(t, f, f.pr, newBase, f.head)
	baseAnswer := askHTTP(t, s, f.pr.ID, baseInput)
	observeHTTP(t, s, baseAnswer, false)
	newHead := sourceGit(t, f.realGit, f.upstream, "head advanced\n", "commit-tree", f.tree, "-p", f.base)
	headInput := prepareQuestionRevision(t, f, f.pr, newBase, newHead)
	headAnswer := askHTTP(t, s, f.pr.ID, headInput)
	observeHTTP(t, s, headAnswer, false)
	if original.RevisionID == baseAnswer.RevisionID || headAnswer.RevisionID == baseAnswer.RevisionID || baseInput.Revision.HeadSHA != f.head {
		t.Fatal("base-only or head-only refresh reused a comparison binding")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := questionPost(t, ctx, s, "/api/reviews/"+f.pr.ID+"/threads/"+original.ThreadID+"/answers", question.FollowupRequest{RevisionID: original.RevisionID, Question: "Continue the original pinned thread."})
	var followup question.Answer
	if r.StatusCode != 202 || json.NewDecoder(r.Body).Decode(&followup) != nil || followup.ThreadID != original.ThreadID || followup.RevisionID != original.RevisionID || followup.Reference.Anchor != f.input.Anchor {
		t.Fatal("HTTP follow-up rebound to refreshed source")
	}
	if terminal := observeHTTP(t, s, followup, false); terminal.State != question.Completed {
		t.Fatal("original pinned follow-up did not complete")
	}
	entries := nativeQuestionAudit(t, f.audit)
	if countNativeSessions(entries) != 3 {
		t.Fatal("old follow-up silently replaced its native session")
	}
	prompts := []string{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Path, "/prompt") {
			prompts = append(prompts, entry.Path)
		}
	}
	if len(prompts) != 4 || prompts[0] != prompts[3] || prompts[0] == prompts[1] || prompts[1] == prompts[2] {
		t.Fatal("native execution lost exact revision/session routing")
	}
}

func TestHTTPFollowupStateLossNeverCreatesReplacementSourceOrSession(t *testing.T) {
	for _, loss := range []string{"cache", "missing-session", "changed-session"} {
		t.Run(loss, func(t *testing.T) {
			f := newQuestionFixture(t, "answer-delayed")
			s := questionHTTP(t, f)
			answer := askHTTP(t, s, f.pr.ID, f.input)
			before := observeHTTP(t, s, answer, false)
			status := 503
			if loss == "cache" {
				path := filepath.Join(f.paths.Cache, "worktrees", f.pr.ID, f.head)
				if err := os.Rename(path, path+".unavailable"); err != nil {
					t.Fatal(err)
				}
				status = 410
			} else if err := os.WriteFile(f.control, []byte(loss), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r := questionPost(t, ctx, s, "/api/reviews/"+f.pr.ID+"/threads/"+answer.ThreadID+"/answers", question.FollowupRequest{RevisionID: answer.RevisionID, Question: "Do not replace lost state."})
			if r.StatusCode != status {
				t.Fatal("lost original state admitted replacement work")
			}
			after := observeHTTP(t, s, answer, false)
			if after.Text != before.Text || after.State != question.Completed || countNativeSessions(nativeQuestionAudit(t, f.audit)) != 1 {
				t.Fatal("state loss erased history or replaced its native session")
			}
		})
	}
}

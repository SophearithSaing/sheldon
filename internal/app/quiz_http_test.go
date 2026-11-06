package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// readQuizHTTP checks the real protected public projection, never private storage DTOs.
func readQuizHTTP(t *testing.T, s *httpapi.Server, base string) quiz.ReadResult {
	t.Helper()
	response := historyGET(t, s, base)
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("quiz read status %d", response.StatusCode)
	}
	for _, private := range []string{"correctChoiceID", "explanation", "evidence", "surrounding", "schemaVersion", "ses_fixture", "sheldon-opencode-", "RAW PROVIDER", "HIDDEN"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("unchecked read leaked %s", private)
		}
	}
	var result quiz.ReadResult
	if json.Unmarshal(data, &result) != nil || result.Validate(result.PRID) != nil {
		t.Fatal("invalid public quiz DTO")
	}
	return result
}

// admitQuizHTTP starts one actual app-owned generation with an explicit saved predecessor.
func admitQuizHTTP(t *testing.T, f questionFixture, s *httpapi.Server, version string) quiz.Status {
	t.Helper()
	response := questionPost(t, context.Background(), s, "/api/reviews/"+f.pr.ID+"/quiz/generations", map[string]any{"revision": f.input.Revision, "expectedVersion": version})
	var admitted quiz.Status
	if response.StatusCode != 202 || json.NewDecoder(response.Body).Decode(&admitted) != nil || admitted.Validate(f.pr.ID) != nil || admitted.State != quiz.Pending {
		t.Fatalf("quiz admission status %d", response.StatusCode)
	}
	return admitted
}

// waitQuizHTTP polls only one known attempt without resubmission or exposure of structured output.
func waitQuizHTTP(t *testing.T, s *httpapi.Server, base string, admitted quiz.Status) quiz.Status {
	t.Helper()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		response := historyGET(t, s, base+"/generations/"+admitted.GenerationID)
		var status quiz.Status
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&status) != nil || status.Validate(admitted.PRID) != nil {
			t.Fatal("invalid actual generation feedback")
		}
		response.Body.Close()
		switch status.State {
		case quiz.Completed, quiz.Failed, quiz.Cancelled, quiz.Unknown:
			return status
		}
		select {
		case <-deadline.C:
			t.Fatal("HTTP generation did not finish")
		case <-tick.C:
		}
	}
}

func TestQuizHTTPReadsAndChecksSavedHistoryWithoutSourceOrNativeWork(t *testing.T) {
	paths := testPaths(t)
	owner := ownedPRInstance(t, paths.Data)
	saved := quizStoreFixture(t, owner, "https://github.com/owner/repo/pull/1")
	if err := owner.SaveQuiz(saved, 0); err != nil {
		t.Fatal(err)
	}
	owner.close()
	owner = ownedPRInstance(t, paths.Data)
	t.Setenv("PATH", "")
	service := &sourceRegistry{instance: owner, paths: paths}
	s := questionHTTP(t, questionFixture{owner: owner, service: service})
	base := "/api/reviews/" + saved.PRID + "/quiz"
	owner.quizStoreMu.Lock()
	owner.quizStoreUncertain = map[string]bool{saved.PRID: true}
	owner.quizStoreMu.Unlock()
	r, _ := http.NewRequest("HEAD", s.URL()+base, nil)
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 405 || !owner.quizStoreUncertain[saved.PRID] {
		t.Fatal("HEAD read/cleared uncertainty")
	}
	view := readQuizHTTP(t, s, base)
	if view.Quiz == nil || view.Version != "1" || view.Generation != nil || view.Quiz.ID != saved.QuizID {
		t.Fatal("source-free saved set unavailable")
	}
	path := filepath.Join(paths.Data, "reviews", saved.PRID, "quiz.json")
	before, _ := os.ReadFile(path)
	for _, choice := range []string{"a", "b"} {
		response := questionPost(t, context.Background(), s, base+"/"+saved.QuizID+"/questions/q1/check", map[string]string{"revisionID": saved.RevisionID, "choiceID": choice})
		var result quiz.Checked
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&result) != nil || result.Validate(saved.PRID) != nil || result.Correct != (choice == "b") || result.Evidence[0].Selected[0].Text != "<literal source>界\r" {
			t.Fatal("actual saved-key grading failed")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || owner.cache != nil || owner.quizzes != nil || owner.questions != nil {
		t.Fatal("read/check wrote attempts or started native/source work")
	}
	if _, err := os.Stat(paths.Cache); !os.IsNotExist(err) {
		t.Fatal("read/check created cache")
	}
	if response := historyGET(t, s, base+"/generations/"+strings.Repeat("5", 64)); response.StatusCode != 404 {
		t.Fatal("restart invented generation observation")
	}
	next := saved
	next.Version = 2
	next.QuizID = strings.Repeat("8", 64)
	if err := owner.SaveQuiz(next, 1); err != nil {
		t.Fatal(err)
	}
	response = questionPost(t, context.Background(), s, base+"/"+saved.QuizID+"/questions/q1/check", map[string]string{"revisionID": saved.RevisionID, "choiceID": "b"})
	if response.StatusCode != 409 {
		t.Fatal("stale check graded replacement")
	}
	bad := []byte("{private corrupt diagnostic")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	response = historyGET(t, s, base)
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 500 || bytes.Contains(data, []byte("private corrupt")) {
		t.Fatal("corrupt quiz became healthy or leaked bytes")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, bad) {
		t.Fatal("HTTP read reset corrupt quiz")
	}
}

func TestQuizHTTPGeneratesChecksAndRejectsStaleReplacementWithoutReplay(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	s := questionHTTP(t, f)
	base := "/api/reviews/" + f.pr.ID + "/quiz"
	if empty := readQuizHTTP(t, s, base); empty.Quiz != nil || empty.Version != "0" || empty.Generation != nil || len(nativeQuestionAudit(t, f.audit)) != 0 {
		t.Fatal("empty read created native work")
	}
	admitted := admitQuizHTTP(t, f, s, "0")
	final := waitQuizHTTP(t, s, base, admitted)
	if final.State != quiz.Completed || final.SavedVersion != "1" || quizPromptCount(t, f) != 1 {
		t.Fatal("HTTP generation did not save exactly once")
	}
	view := readQuizHTTP(t, s, base)
	if view.Quiz.ID != admitted.QuizID || view.Generation == nil || view.Generation.GenerationID != admitted.GenerationID {
		t.Fatal("saved set and runtime feedback lost scope")
	}
	nativeCount := len(nativeQuestionAudit(t, f.audit))
	response := questionPost(t, context.Background(), s, base+"/"+admitted.QuizID+"/questions/q1/check", map[string]string{"revisionID": admitted.RevisionID, "choiceID": "a"})
	var checked quiz.Checked
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&checked) != nil || checked.Correct || checked.CorrectChoiceID != "b" {
		t.Fatal("saved backend key not used")
	}
	readQuizHTTP(t, s, base)
	if len(nativeQuestionAudit(t, f.audit)) != nativeCount {
		t.Fatal("read/check performed native requests")
	}
	response = questionPost(t, context.Background(), s, base+"/generations", map[string]any{"revision": f.input.Revision, "expectedVersion": "0"})
	if response.StatusCode != 409 || quizPromptCount(t, f) != 1 {
		t.Fatal("stale admission replayed generation")
	}
	second := admitQuizHTTP(t, f, s, "1")
	if waitQuizHTTP(t, s, base, second).State != quiz.Completed || quizPromptCount(t, f) != 2 {
		t.Fatal("explicit replacement failed")
	}
	response = questionPost(t, context.Background(), s, base+"/"+admitted.QuizID+"/questions/q1/check", map[string]string{"revisionID": admitted.RevisionID, "choiceID": "a"})
	if response.StatusCode != 409 {
		t.Fatal("old question silently retargeted")
	}
	if response := historyGET(t, s, base+"/generations/"+admitted.GenerationID); response.StatusCode != 404 {
		t.Fatal("retired generation ID remained a mutable target")
	}
}

func TestQuizHTTPBusyAndCancelTargetOnlyAdmittedGeneration(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	s := questionHTTP(t, f)
	base := "/api/reviews/" + f.pr.ID + "/quiz"
	admitted := admitQuizHTTP(t, f, s, "0")
	awaitQuizPrompts(t, f, 1)
	response := questionPost(t, context.Background(), s, base+"/generations", map[string]any{"revision": f.input.Revision, "expectedVersion": "0"})
	if response.StatusCode != 409 || quizPromptCount(t, f) != 1 {
		t.Fatal("HTTP busy gate queued work")
	}
	response = questionPost(t, context.Background(), s, base+"/generations/"+admitted.GenerationID+"/cancel", map[string]string{"revisionID": strings.Repeat("9", 64)})
	if response.StatusCode != 404 {
		t.Fatal("wrong revision cancellation accepted")
	}
	response = questionPost(t, context.Background(), s, base+"/generations/"+admitted.GenerationID+"/cancel", map[string]string{"revisionID": admitted.RevisionID})
	var ack quiz.Status
	if response.StatusCode != 202 || json.NewDecoder(response.Body).Decode(&ack) != nil || !ack.CancelRequested {
		t.Fatal("exact cancellation not acknowledged")
	}
	if final := waitQuizHTTP(t, s, base, admitted); final.State != quiz.Cancelled || final.ExecutionUnknown || quizPromptCount(t, f) != 1 {
		t.Fatal("HTTP Cancel did not reconcile the exact answer")
	}
	if readQuizHTTP(t, s, base).Quiz != nil {
		t.Fatal("cancellation fabricated a saved set")
	}
}

func TestQuizHTTPCheckRacingReplacementKeepsExactSet(t *testing.T) {
	paths := testPaths(t)
	owner := ownedPRInstance(t, paths.Data)
	saved := quizStoreFixture(t, owner, "https://github.com/owner/repo/pull/1")
	if err := owner.SaveQuiz(saved, 0); err != nil {
		t.Fatal(err)
	}
	service := &sourceRegistry{instance: owner, paths: paths}
	s := questionHTTP(t, questionFixture{owner: owner, service: service})
	for n := range 8 {
		next := saved
		next.Version++
		next.QuizID = fmt.Sprintf("%064x", n+100)
		body, err := json.Marshal(map[string]string{"revisionID": saved.RevisionID, "choiceID": "b"})
		if err != nil {
			t.Fatal(err)
		}
		path := "/api/reviews/" + saved.PRID + "/quiz/" + saved.QuizID + "/questions/q1/check"
		r, err := http.NewRequest("POST", s.URL()+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+s.APIToken())
		r.Header.Set("Content-Type", "application/json")
		start := make(chan struct{})
		replaced := make(chan error, 1)
		go func() { <-start; replaced <- owner.SaveQuiz(next, saved.Version) }()
		close(start)
		response, requestErr := http.DefaultClient.Do(r)
		if err := <-replaced; err != nil {
			t.Fatal(err)
		}
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch response.StatusCode {
		case 200:
			var checked quiz.Checked
			if json.Unmarshal(data, &checked) != nil || checked.QuizID != saved.QuizID || checked.Version != fmt.Sprint(saved.Version) || !checked.Correct {
				t.Fatal("racing check retargeted replacement")
			}
		case 409:
		default:
			t.Fatalf("racing check returned %d", response.StatusCode)
		}
		saved = next
	}
	if owner.quizzes != nil || owner.cache != nil || owner.questions != nil {
		t.Fatal("racing saved checks started source/native work")
	}
}

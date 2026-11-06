package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

type quizReadFixture struct {
	*testPRRegistry
	saved         quiz.Saved
	err           error
	calls         atomic.Int32
	viewOverride  *quiz.ReadResult
	checkOverride *quiz.Checked
}

// ReadQuizView exposes only a synthetic saved projection, with no generation capabilities.
func (f *quizReadFixture) ReadQuizView(string) (quiz.ReadResult, error) {
	f.calls.Add(1)
	if f.viewOverride != nil {
		return *f.viewOverride, f.err
	}
	view, err := f.saved.Public()
	if f.err != nil {
		return quiz.ReadResult{}, f.err
	}
	return view, err
}

// CheckQuizAnswer grades a synthetic private record without starting a native service.
func (f *quizReadFixture) CheckQuizAnswer(_ string, id, qid, revision, choice string) (quiz.Checked, error) {
	f.calls.Add(1)
	if f.err != nil {
		return quiz.Checked{}, f.err
	}
	if f.checkOverride != nil {
		return *f.checkOverride, nil
	}
	return f.saved.Check(id, qid, revision, choice)
}

type quizRegistryFixture struct {
	*quizReadFixture
	status quiz.Status
}

// GenerateQuiz returns fixture admission only, never model output or fabricated completion.
func (f *quizRegistryFixture) GenerateQuiz(context.Context, string, githubpr.Revision, uint64) (quiz.Status, error) {
	f.calls.Add(1)
	return f.status, f.err
}

// ReadQuizGeneration returns one synthetic finite status without native work.
func (f *quizRegistryFixture) ReadQuizGeneration(string, string) (quiz.Status, error) {
	f.calls.Add(1)
	return f.status, f.err
}

// CancelQuizGeneration acknowledges a synthetic request without claiming termination.
func (f *quizRegistryFixture) CancelQuizGeneration(string, string, string) (quiz.Status, error) {
	f.calls.Add(1)
	status := f.status
	status.CancelRequested = true
	return status, f.err
}

// quizRegistry builds a keyed private fixture with backend-derived quote shapes.
func quizRegistry(t *testing.T) *quizRegistryFixture {
	t.Helper()
	h := historyRegistry(t)
	c := h.detail.Context
	a := quiz.Anchor{FileID: c.Anchor.FileID, HunkID: c.Anchor.HunkID, Side: "new", Start: 1, End: 1}
	e := quiz.Evidence{Anchor: a, OldPath: c.OldPath, NewPath: c.NewPath, Selected: c.Selected, Surrounding: c.Surrounding}
	s := quiz.Saved{SchemaVersion: 1, PRID: h.list.PRID, Version: ^uint64(0), QuizID: strings.Repeat("4", 64), Content: quiz.Content{Revision: c.Revision, RevisionID: c.RevisionID, Questions: []quiz.Question{}}}
	for i := range 5 {
		s.Questions = append(s.Questions, quiz.Question{ID: "q" + string(rune('1'+i)), Prompt: "Fixture question " + string(rune('1'+i)), Choices: []quiz.Choice{{ID: "a", Text: "First"}, {ID: "b", Text: "Second"}, {ID: "c", Text: "Third"}, {ID: "d", Text: "Fourth"}}, CorrectChoiceID: "b", Explanation: "PRIVATE_EXPLANATION_" + string(rune('1'+i)), Evidence: []quiz.Evidence{e}})
	}
	if _, err := s.Encode(); err != nil {
		t.Fatal(err)
	}
	return &quizRegistryFixture{quizReadFixture: &quizReadFixture{testPRRegistry: h.testPRRegistry, saved: s}, status: quiz.Status{PRID: s.PRID, RevisionID: s.RevisionID, GenerationID: strings.Repeat("5", 64), QuizID: strings.Repeat("6", 64), Sequence: "1", State: quiz.Pending}}
}

// quizHTTP sends only synthetic bearer credentials in headers and returns a finite response.
func quizHTTP(t *testing.T, s *Server, client *http.Client, method, path, body, token, contentType string) (int, []byte, http.Header) {
	t.Helper()
	r, err := http.NewRequest(method, s.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("quiz response could be cached")
	}
	return response.StatusCode, data, response.Header
}

func TestQuizReadOnlyProviderWithholdsKeysAndChecksOneQuestion(t *testing.T) {
	f := quizRegistry(t)
	s, err := Listen("", "", f.quizReadFixture)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	base := "/api/reviews/" + f.saved.PRID + "/quiz"
	status, data, _ := quizHTTP(t, s, client, "GET", base, "", s.APIToken(), "")
	if status != 200 || !strings.Contains(string(data), `"version":"18446744073709551615"`) {
		t.Fatal("invalid public read/version")
	}
	for _, secret := range []string{"correctChoiceID", "PRIVATE_EXPLANATION", "evidence", "surrounding", "schemaVersion", "native", "directory"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("unchecked read leaked %s", secret)
		}
	}
	for _, choice := range []string{"a", "b"} {
		body := `{"revisionID":"` + f.saved.RevisionID + `","choiceID":"` + choice + `"}`
		status, data, _ = quizHTTP(t, s, client, "POST", base+"/"+f.saved.QuizID+"/questions/q1/check", body, s.APIToken(), "application/json")
		var result quiz.Checked
		if status != 200 || json.Unmarshal(data, &result) != nil || result.Correct != (choice == "b") || result.CorrectChoiceID != "b" || result.Validate(f.saved.PRID) != nil {
			t.Fatal("incorrect explicit check")
		}
		if strings.Contains(string(data), "PRIVATE_EXPLANATION_2") {
			t.Fatal("check revealed another question")
		}
	}
	body, _ := json.Marshal(map[string]any{"revision": f.saved.Revision, "expectedVersion": "0"})
	if status, _, _ := quizHTTP(t, s, client, "POST", base+"/generations", string(body), s.APIToken(), "application/json"); status != 503 {
		t.Fatal("read-only provider required/started inference")
	}
}

func TestQuizRoutesRejectUnauthorizedAndMalformedRequestsBeforeProviderWork(t *testing.T) {
	f := quizRegistry(t)
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	base := "/api/reviews/" + f.saved.PRID + "/quiz"
	genBody, _ := json.Marshal(map[string]any{"revision": f.saved.Revision, "expectedVersion": "0"})
	routes := []struct {
		method, path, body string
		success            int
	}{
		{"GET", base, "", 200}, {"GET", base + "/generations/" + f.status.GenerationID, "", 200},
		{"POST", base + "/generations", string(genBody), 202},
		{"POST", base + "/generations/" + f.status.GenerationID + "/cancel", `{"revisionID":"` + f.saved.RevisionID + `"}`, 202},
		{"POST", base + "/" + f.saved.QuizID + "/questions/q1/check", `{"revisionID":"` + f.saved.RevisionID + `","choiceID":"a"}`, 200},
	}
	for _, route := range routes {
		for _, fault := range []string{"missing bearer", "wrong bearer", "cookie", "origin", "host", "fetch metadata", "query bearer", "empty query", "encoded path", "body", "media", "valid"} {
			t.Run(route.method+route.path+"/"+fault, func(t *testing.T) {
				method, path, body := route.method, route.path, route.body
				want := route.success
				if fault == "query bearer" {
					path += "?token=secret"
					want = 404
				}
				if fault == "empty query" {
					path += "?"
					want = 404
				}
				if fault == "encoded path" {
					path = strings.Replace(path, "/quiz", "/%71uiz", 1)
					want = 404
				}
				if fault == "body" {
					body = "{} {}"
					want = 400
				}
				r, err := http.NewRequest(method, s.URL()+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+s.APIToken())
				r.Header.Set("Content-Type", "application/json")
				switch fault {
				case "missing bearer":
					r.Header.Del("Authorization")
					want = 401
				case "wrong bearer":
					r.Header.Set("Authorization", "Bearer wrong")
					want = 401
				case "cookie":
					r.Header.Del("Authorization")
					r.Header.Set("Cookie", "token="+s.APIToken())
					want = 401
				case "origin":
					r.Header.Set("Origin", "http://evil.invalid")
					want = 403
				case "host":
					r.Host = "evil.invalid"
					want = 403
				case "fetch metadata":
					r.Header.Set("Sec-Fetch-Site", "cross-site")
					want = 403
				case "media":
					if method == "POST" {
						r.Header.Set("Content-Type", "text/plain")
						want = 415
					}
				}
				before := f.calls.Load()
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("%s: got %d want %d", fault, response.StatusCode, want)
				}
				if want >= 400 && f.calls.Load() != before {
					t.Fatal("rejected request reached quiz provider")
				}
				if strings.Contains(string(data), s.APIToken()) {
					t.Fatal("quiz response reflected bearer")
				}
			})
		}
		if route.method == "GET" {
			before := f.calls.Load()
			if status, _, _ := quizHTTP(t, s, client, "HEAD", route.path, "", s.APIToken(), ""); status != 405 || f.calls.Load() != before {
				t.Fatal("HEAD read/mutated provider state")
			}
		} else {
			for _, bad := range []string{`null`, `{broken`, strings.Replace(route.body, `"revision`, `"Revision`, 1), strings.Replace(route.body, `{`, `{"model":"private",`, 1), strings.Replace(route.body, `{`, `{"revisionID":"`+f.saved.RevisionID+`",`, 1), strings.Repeat(" ", 4097)} {
				before := f.calls.Load()
				status, _, _ := quizHTTP(t, s, client, "POST", route.path, bad, s.APIToken(), "application/json")
				if status != 400 && status != 413 || f.calls.Load() != before {
					t.Fatal("malformed/duplicate/oversized mutation reached provider")
				}
			}
		}
	}
}

func TestQuizErrorsScopesAndProviderResultsFailClosed(t *testing.T) {
	f := quizRegistry(t)
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	base := "/api/reviews/" + f.saved.PRID + "/quiz"
	for _, tc := range []struct {
		err    error
		status int
	}{
		{quiz.ErrNotFound, 404}, {quiz.ErrBusy, 409}, {quiz.ErrConflict, 409}, {quiz.ErrUnknown, 409}, {quiz.ErrSource, 410}, {quiz.ErrLimit, 422}, {quiz.ErrClosed, 503}, {quiz.ErrSaveUncertain, 500}, {errors.New("PRIVATE /secret provider diagnostic"), 500},
	} {
		f.err = tc.err
		status, data, _ := quizHTTP(t, s, client, "GET", base, "", s.APIToken(), "")
		if status != tc.status || len(data) > 4096 || strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "/secret") {
			t.Fatal("error mapping leaked diagnostics")
		}
	}
	f.err = nil
	for _, path := range []string{"/api/reviews/" + strings.Repeat("9", 64) + "/quiz", base + "/generations/not-an-id", base + "/" + f.saved.QuizID + "/questions/q6/check"} {
		method := "GET"
		if strings.HasSuffix(path, "/check") {
			method = "POST"
		}
		before := f.calls.Load()
		status, _, _ := quizHTTP(t, s, client, method, path, `{}`, s.APIToken(), "application/json")
		if status != 404 || f.calls.Load() != before {
			t.Fatal("invalid scope reached provider")
		}
	}
	f.status.GenerationID = strings.Repeat("8", 64)
	if status, _, _ := quizHTTP(t, s, client, "GET", base+"/generations/"+strings.Repeat("5", 64), "", s.APIToken(), ""); status != 500 {
		t.Fatal("wrong-scope provider status accepted")
	}
	view, _ := f.saved.Public()
	view.Quiz.Questions[0].Prompt = strings.Repeat("x", quiz.MaxReadBytes)
	f.viewOverride = &view
	if status, data, _ := quizHTTP(t, s, client, "GET", base, "", s.APIToken(), ""); status != 500 || len(data) > 4096 {
		t.Fatal("oversized public response partially serialized")
	}
	checked, _ := f.saved.Check(f.saved.QuizID, "q1", f.saved.RevisionID, "a")
	checked.Correct = true
	f.checkOverride = &checked
	if status, _, _ := quizHTTP(t, s, client, "POST", base+"/"+f.saved.QuizID+"/questions/q1/check", `{"revisionID":"`+f.saved.RevisionID+`","choiceID":"a"}`, s.APIToken(), "application/json"); status != 500 {
		t.Fatal("inconsistent provider grade accepted")
	}
}

func TestQuizCheckEncodedBudgetFailsBeforeSuccessHeaders(t *testing.T) {
	f := quizRegistry(t)
	checked, err := f.saved.Check(f.saved.QuizID, "q1", f.saved.RevisionID, "a")
	if err != nil {
		t.Fatal(err)
	}
	e := checked.Evidence[0]
	e.Surrounding.Old.Start, e.Surrounding.Old.Count = 0, 0
	e.Surrounding.New.Start, e.Surrounding.New.Count = 1, 3000
	e.Surrounding.Lines = make([]git.DiffLine, 3000)
	for i := range e.Surrounding.Lines {
		e.Surrounding.Lines[i] = git.DiffLine{ID: fmt.Sprintf("%064x", i+1), Sign: "+", NewLine: i + 1}
	}
	e.Anchor.Start, e.Anchor.End = 1, 1
	e.Selected = []git.DiffLine{e.Surrounding.Lines[0]}
	second := e
	second.Anchor.Start, second.Anchor.End = 2, 2
	second.Selected = []git.DiffLine{e.Surrounding.Lines[1]}
	checked.Evidence = []quiz.Evidence{e, second}
	data, err := json.Marshal(checked)
	if err != nil || checked.Validate(f.saved.PRID) != nil || len(data)+1 <= quiz.MaxCheckBytes {
		t.Fatal("fixture must pass raw evidence bounds but exceed encoded check budget")
	}
	f.checkOverride = &checked
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	path := "/api/reviews/" + f.saved.PRID + "/quiz/" + f.saved.QuizID + "/questions/q1/check"
	status, data, _ := quizHTTP(t, s, client, "POST", path, `{"revisionID":"`+f.saved.RevisionID+`","choiceID":"a"}`, s.APIToken(), "application/json")
	if status != 500 || len(data) > 4096 || !strings.Contains(string(data), `"code":"result-too-large"`) || strings.Contains(string(data), "PRIVATE_EXPLANATION") {
		t.Fatal("encoded overflow partially published a checked result")
	}
}

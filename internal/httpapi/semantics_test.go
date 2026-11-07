package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/semantic"
)

type semanticFixture struct {
	*testPRRegistry
	status semantic.Status
	calls  atomic.Int32
}

// ReadSemanticView supplies empty saved state without source/native work.
func (f *semanticFixture) ReadSemanticView(prID string) (semantic.ReadResult, error) {
	f.calls.Add(1)
	return semantic.ReadResult{PRID: prID, Version: "0"}, nil
}

// GenerateSemantic supplies fixture admission, not native inference.
func (f *semanticFixture) GenerateSemantic(context.Context, string, githubpr.Revision, uint64) (semantic.Status, error) {
	f.calls.Add(1)
	return f.status, nil
}

// ReadSemanticGeneration returns finite exact-attempt fixture feedback.
func (f *semanticFixture) ReadSemanticGeneration(string, string) (semantic.Status, error) {
	f.calls.Add(1)
	return f.status, nil
}

// CancelSemanticGeneration records only fixture cancellation intent.
func (f *semanticFixture) CancelSemanticGeneration(string, string, string) (semantic.Status, error) {
	f.calls.Add(1)
	s := f.status
	s.CancelRequested = true
	return s, nil
}

func TestSemanticRoutesAndSecurity(t *testing.T) {
	h := historyRegistry(t)
	c := h.detail.Context
	prID := h.list.PRID
	f := &semanticFixture{testPRRegistry: h.testPRRegistry, status: semantic.Status{PRID: prID, GenerationID: strings.Repeat("5", 64), ReviewID: strings.Repeat("6", 64), RevisionID: c.RevisionID, Sequence: "1", State: "pending"}}
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve()
	defer s.Shutdown(context.Background())
	base := "/api/reviews/" + prID + "/semantic-review"
	admission, _ := json.Marshal(map[string]any{"revision": c.Revision, "expectedVersion": "0"})
	cancel, _ := json.Marshal(map[string]any{"revisionID": c.RevisionID})
	for _, test := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", base, "", 200}, {"HEAD", base, "", 405}, {"GET", base + "?", "", 404}, {"GET", base, "{}", 400},
		{"POST", base + "/generations", string(admission), 202}, {"GET", base + "/generations/" + f.status.GenerationID, "", 200}, {"POST", base + "/generations/" + f.status.GenerationID + "/cancel", string(cancel), 202},
		{"POST", base + "/generations", `{"expectedVersion":"00"}`, 400}, {"POST", base + "/generations", `{"revision":null,"revision":null}`, 400}, {"POST", base + "/generations", strings.Repeat("x", 4097), 413},
	} {
		t.Run(test.method+test.path+test.body[:min(len(test.body), 20)], func(t *testing.T) {
			before := f.calls.Load()
			status, _, _ := quizHTTP(t, s, http.DefaultClient, test.method, test.path, test.body, s.APIToken(), "application/json")
			if status != test.want {
				t.Fatalf("status %d want %d", status, test.want)
			}
			if test.want >= 400 && f.calls.Load() != before {
				t.Fatal("invalid request reached provider")
			}
		})
	}
	for _, token := range []string{"", "wrong"} {
		status, _, _ := quizHTTP(t, s, http.DefaultClient, "GET", base, "", token, "")
		if status != 401 {
			t.Fatal("missing/wrong bearer accepted")
		}
	}
	for _, header := range []string{"Origin", "Host"} {
		req, _ := http.NewRequest("GET", s.URL()+base, nil)
		req.Header.Set("Authorization", "Bearer "+s.APIToken())
		if header == "Host" {
			req.Host = "evil.example"
		} else {
			req.Header.Set(header, "https://evil.example")
		}
		before := f.calls.Load()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 || f.calls.Load() != before {
			t.Fatal("hostile request reached provider")
		}
	}
}

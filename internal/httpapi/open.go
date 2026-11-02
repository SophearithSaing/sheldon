package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// PRRegistry provides the app-owned durable PR index.
type PRRegistry interface {
	RegisterPR(string) (githubpr.PullRequest, error)
	ReadPR(string) (githubpr.PullRequest, error)
}

// OpenResult identifies a registered PR, not fetched source or an active review.
type OpenResult struct {
	PRID  string `json:"prID"`
	Route string `json:"route"`
}

// open registers a bounded PR-open request without changing existing tabs.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "open query parameters are not supported")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "JSON body required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if end := decoder.Decode(&extra); !errors.Is(end, io.EOF) {
			err = errors.New("unexpected request data")
			if end != nil {
				err = end
			}
		}
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "open body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid open body")
		}
		return
	}
	expected, err := githubpr.ParseURL(input.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid GitHub PR URL")
		return
	}
	if s.prs == nil {
		writeError(w, http.StatusServiceUnavailable, "PR registry is unavailable")
		return
	}
	pr, err := s.prs.RegisterPR(input.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PR registration failed; check the private prs.json index and retry")
		return
	}
	route, err := githubpr.ReviewPath(pr.ID)
	if err != nil || pr.ID != expected.ID || pr.Validate() != nil {
		writeError(w, http.StatusInternalServerError, "invalid registered PR")
		return
	}
	writeJSON(w, http.StatusAccepted, OpenResult{PRID: pr.ID, Route: route})
}

// review reads one registered PR without fetching source or inspecting credentials.
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !githubpr.ValidID(id) || r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path {
		http.NotFound(w, r)
		return
	}
	if s.prs == nil {
		writeError(w, http.StatusServiceUnavailable, "PR registry is unavailable")
		return
	}
	pr, err := s.prs.ReadPR(id)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "PR is not registered in this Sheldon instance")
		return
	}
	if err != nil || pr.ID != id || pr.Validate() != nil {
		writeError(w, http.StatusInternalServerError, "cannot read PR; check the private prs.json index and retry")
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

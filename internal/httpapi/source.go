package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

// SourceRequest distinguishes metadata resolution from explicit Git-only consent/selection.
type SourceRequest struct {
	Action   string             `json:"action"`
	Branch   string             `json:"branch,omitempty"`
	SHA      string             `json:"sha,omitempty"`
	Revision *githubpr.Revision `json:"revision,omitempty"`
}

// SourceCandidate is a fetched upstream branch, never an arbitrary remote URL.
type SourceCandidate struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

// SourceResult separates unresolved metadata/selection from complete pinned source.
type SourceResult struct {
	State               string               `json:"state"`
	PR                  githubpr.PullRequest `json:"pr"`
	Candidates          []SourceCandidate    `json:"candidates,omitempty"`
	Diff                *git.Diff            `json:"diff,omitempty"`
	RequestedRevisionID string               `json:"requestedRevisionID,omitempty"`
}

// SourceProvider loads one registered PR using the app-owned cache lifetime.
type SourceProvider interface {
	LoadSource(context.Context, githubpr.PullRequest, SourceRequest) (SourceResult, error)
}

// source accepts explicit source work after authenticating and looking up the routed PR.
func (s *Server) source(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !githubpr.ValidID(id) || r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path {
		http.NotFound(w, r)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "JSON body required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input SourceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if end := decoder.Decode(&extra); !errors.Is(end, io.EOF) {
			err = errors.New("unexpected source data")
			if end != nil {
				err = end
			}
		}
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "source body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid source body")
		}
		return
	}
	if !validSourceRequest(input, id) {
		writeError(w, http.StatusBadRequest, "invalid source action or base selection")
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
		writeError(w, http.StatusInternalServerError, "cannot read registered PR")
		return
	}
	provider, ok := s.prs.(SourceProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "source loading is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(100 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusInternalServerError, "cannot start source response")
		return
	}
	result, err := provider.LoadSource(ctx, pr, input)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	if err := ctx.Err(); err != nil {
		writeSourceError(w, err)
		return
	}
	if !validSourceResult(result, id) {
		writeError(w, http.StatusInternalServerError, "invalid source result")
		return
	}
	if input.Revision != nil {
		result.RequestedRevisionID, _ = input.Revision.ID()
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > 32<<20 {
		writeSourceError(w, process.ErrOutputLimit)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// validSourceRequest restricts each action to its exact route-bound input shape.
func validSourceRequest(input SourceRequest, id string) bool {
	switch input.Action {
	case "load", "git-only":
		return input.Revision == nil && input.Branch == "" && input.SHA == ""
	case "select-base":
		return input.Revision == nil && githubpr.ValidBranch(input.Branch) && githubpr.ValidCommit(input.SHA)
	case "refresh":
		return input.Branch == "" && input.SHA == "" && input.Revision != nil && input.Revision.Validate(id) == nil
	default:
		return false
	}
}

// validSourceResult rejects partial or mismatched success states before serialization.
func validSourceResult(result SourceResult, id string) bool {
	if result.PR.ID != id || result.PR.Validate() != nil {
		return false
	}
	switch result.State {
	case "fallback-required":
		return result.Diff == nil && len(result.Candidates) == 0
	case "base-selection-required":
		if result.Diff != nil || len(result.Candidates) == 0 || len(result.Candidates) > 1000 {
			return false
		}
		seen := make(map[string]bool)
		for _, candidate := range result.Candidates {
			if !githubpr.ValidBranch(candidate.Branch) || !githubpr.ValidCommit(candidate.SHA) || seen[candidate.Branch] {
				return false
			}
			seen[candidate.Branch] = true
		}
		return true
	case "ready":
		if result.Diff == nil || !result.Diff.Complete || result.Diff.Revision.Validate(id) != nil || len(result.Candidates) != 0 {
			return false
		}
		revisionID, err := result.Diff.Revision.ID()
		return err == nil && revisionID == result.Diff.RevisionID
	default:
		return false
	}
}

// writeSourceError exposes fixed recovery categories without native output or cache paths.
func writeSourceError(w http.ResponseWriter, err error) {
	code, message, status := "source-unavailable", "Cannot prepare source. Check Git access and the managed cache, then retry.", http.StatusBadGateway
	switch {
	case errors.Is(err, git.ErrRefreshRequired):
		code, message, status = "refresh-required", "PR or base changed. Retry loading or fetch branches and select again.", http.StatusConflict
	case errors.Is(err, process.ErrOutputLimit):
		code, message, status = "source-too-large", "Source exceeds the complete-view limits; no partial diff is shown.", http.StatusUnprocessableEntity
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code, message, status = "source-timeout", "Source loading timed out or was cancelled. Retry explicitly.", http.StatusGatewayTimeout
	case errors.Is(err, git.ErrHistoryUnavailable), errors.Is(err, git.ErrUnrelatedHistory), errors.Is(err, git.ErrAmbiguousMergeBase):
		code, message, status = "history-unavailable", "Complete, unambiguous comparison history is unavailable.", http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

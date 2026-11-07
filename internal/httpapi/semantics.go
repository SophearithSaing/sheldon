package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/semantic"
)

// SemanticReadProvider reads saved source/grouping without requiring generation capabilities.
type SemanticReadProvider interface {
	ReadSemanticView(string) (semantic.ReadResult, error)
}

// SemanticGenerationProvider owns explicit admission and exact-attempt observation/cancellation.
type SemanticGenerationProvider interface {
	GenerateSemantic(context.Context, string, githubpr.Revision, uint64) (semantic.Status, error)
	ReadSemanticGeneration(string, string) (semantic.Status, error)
	CancelSemanticGeneration(string, string, string) (semantic.Status, error)
}

// semanticRoute rejects malformed scope before registered-provider access.
func (s *Server) semanticRoute(w http.ResponseWriter, r *http.Request) bool {
	if id := r.PathValue("generation"); id != "" && !githubpr.ValidID(id) {
		http.NotFound(w, r)
		return false
	}
	return s.questionRoute(w, r)
}

// semanticBody reads bounded strict JSON without reflecting request bytes.
func semanticBody(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeSemanticError(w, 415, "json-required")
		return false
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeSemanticError(w, 413, "body-too-large")
		} else {
			writeSemanticError(w, 400, "invalid-request")
		}
		return false
	}
	if semantic.DecodeRequest(b, out) != nil {
		writeSemanticError(w, 400, "invalid-request")
		return false
	}
	return true
}

// semanticRead serves saved content or exact finite feedback without starting work.
func (s *Server) semanticRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeSemanticError(w, 405, "get-required")
		return
	}
	if !s.semanticRoute(w, r) {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeSemanticError(w, 400, "body-not-allowed")
		return
	}
	prID := r.PathValue("id")
	if id := r.PathValue("generation"); id != "" {
		p, ok := s.prs.(SemanticGenerationProvider)
		if !ok {
			writeSemanticError(w, 503, "semantic-unavailable")
			return
		}
		status, err := p.ReadSemanticGeneration(prID, id)
		if err != nil {
			writeSemanticFailure(w, err)
			return
		}
		if status.Validate(prID) != nil || status.GenerationID != id {
			writeSemanticError(w, 500, "invalid-result")
			return
		}
		writeSemanticJSON(w, 200, status, 4096)
		return
	}
	p, ok := s.prs.(SemanticReadProvider)
	if !ok {
		writeSemanticError(w, 503, "semantic-unavailable")
		return
	}
	result, err := p.ReadSemanticView(prID)
	if err != nil {
		writeSemanticFailure(w, err)
		return
	}
	if result.Validate(prID) != nil {
		writeSemanticError(w, 500, "invalid-result")
		return
	}
	writeSemanticJSON(w, 200, result, semantic.MaxContentBytes)
}

// generateSemantic admits one explicit original-revision request without completion claims.
func (s *Server) generateSemantic(w http.ResponseWriter, r *http.Request) {
	if !s.semanticRoute(w, r) {
		return
	}
	var input struct {
		Revision        githubpr.Revision `json:"revision"`
		ExpectedVersion string            `json:"expectedVersion"`
	}
	if !semanticBody(w, r, 4096, &input) {
		return
	}
	expected, err := semantic.ParseVersion(input.ExpectedVersion)
	prID := r.PathValue("id")
	if err != nil || input.Revision.Validate(prID) != nil {
		writeSemanticError(w, 400, "invalid-request")
		return
	}
	p, ok := s.prs.(SemanticGenerationProvider)
	if !ok {
		writeSemanticError(w, 503, "semantic-unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	status, err := p.GenerateSemantic(ctx, prID, input.Revision, expected)
	if err != nil {
		writeSemanticFailure(w, err)
		return
	}
	id, _ := input.Revision.ID()
	if status.Validate(prID) != nil || status.RevisionID != id || status.Sequence != "1" || status.State != "pending" || status.CancelRequested {
		writeSemanticError(w, 500, "invalid-result")
		return
	}
	writeSemanticJSON(w, 202, status, 4096)
}

// cancelSemantic signals only the exact original generation, not another PR or new attempt.
func (s *Server) cancelSemantic(w http.ResponseWriter, r *http.Request) {
	if !s.semanticRoute(w, r) {
		return
	}
	var input struct {
		RevisionID string `json:"revisionID"`
	}
	if !semanticBody(w, r, 1024, &input) {
		return
	}
	if !githubpr.ValidID(input.RevisionID) {
		writeSemanticError(w, 400, "invalid-request")
		return
	}
	p, ok := s.prs.(SemanticGenerationProvider)
	if !ok {
		writeSemanticError(w, 503, "semantic-unavailable")
		return
	}
	status, err := p.CancelSemanticGeneration(r.PathValue("id"), r.PathValue("generation"), input.RevisionID)
	if err != nil {
		writeSemanticFailure(w, err)
		return
	}
	if status.Validate(r.PathValue("id")) != nil || status.GenerationID != r.PathValue("generation") || status.RevisionID != input.RevisionID {
		writeSemanticError(w, 500, "invalid-result")
		return
	}
	writeSemanticJSON(w, 202, status, 4096)
}

// writeSemanticJSON encodes and bounds finite responses before successful headers.
func writeSemanticJSON(w http.ResponseWriter, status int, value any, limit int) {
	b, err := json.Marshal(value)
	if err != nil || len(b)+1 > limit {
		writeSemanticError(w, 500, "result-too-large")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(b, '\n'))
}

// writeSemanticFailure maps failures to application-owned categories without raw diagnostics.
func writeSemanticFailure(w http.ResponseWriter, err error) {
	status, code := 500, "semantic-storage-failed"
	switch {
	case errors.Is(err, semantic.ErrConflict):
		status, code = 409, "review-changed"
	case errors.Is(err, semantic.ErrBusy):
		status, code = 409, "semantic-busy"
	case errors.Is(err, semantic.ErrUnknown):
		status, code = 409, "execution-uncertain"
	case errors.Is(err, semantic.ErrNotFound):
		status, code = 404, "semantic-not-found"
	case errors.Is(err, semantic.ErrSource):
		status, code = 410, "source-unavailable"
	case errors.Is(err, semantic.ErrLimit):
		status, code = 422, "semantic-limit"
	case errors.Is(err, semantic.ErrSaveUncertain):
		code = "save-uncertain"
	case errors.Is(err, semantic.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = 503, "semantic-unavailable"
	}
	writeSemanticError(w, status, code)
}

// writeSemanticError emits fixed feedback instead of request/provider/storage details.
func writeSemanticError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"code": code, "error": "Semantic review request could not be confirmed. Read saved state before retrying uncertain work."})
}

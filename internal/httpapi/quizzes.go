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
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// QuizReadProvider projects/grades saved data without requiring generation capabilities.
type QuizReadProvider interface {
	ReadQuizView(string) (quiz.ReadResult, error)
	CheckQuizAnswer(string, string, string, string, string) (quiz.Checked, error)
}

// QuizGenerationProvider owns explicit admission and exact-attempt read/cancellation.
type QuizGenerationProvider interface {
	GenerateQuiz(context.Context, string, githubpr.Revision, uint64) (quiz.Status, error)
	ReadQuizGeneration(string, string) (quiz.Status, error)
	CancelQuizGeneration(string, string, string) (quiz.Status, error)
}

// quizRoute extends canonical registered-PR checks with exact quiz-specific route identities.
func (s *Server) quizRoute(w http.ResponseWriter, r *http.Request) bool {
	for _, key := range []string{"quiz", "generation"} {
		if value := r.PathValue(key); value != "" && !githubpr.ValidID(value) {
			http.NotFound(w, r)
			return false
		}
	}
	if value := r.PathValue("question"); value != "" && !quiz.ValidQuestionID(value) {
		http.NotFound(w, r)
		return false
	}
	return s.questionRoute(w, r)
}

// quizBody enforces bounded canonical request JSON without reflecting submitted bytes.
func quizBody(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeQuizError(w, 415, "json-required")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeQuizError(w, 413, "body-too-large")
		} else {
			writeQuizError(w, 400, "invalid-request")
		}
		return false
	}
	if quiz.DecodeRequest(data, out) != nil {
		writeQuizError(w, 400, "invalid-request")
		return false
	}
	return true
}

// quizRead serves saved unanswered data or one finite generation snapshot without starting work.
func (s *Server) quizRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeQuizError(w, 405, "get-required")
		return
	}
	if !s.quizRoute(w, r) {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeQuizError(w, 400, "body-not-allowed")
		return
	}
	prID := r.PathValue("id")
	if generationID := r.PathValue("generation"); generationID != "" {
		provider, ok := s.prs.(QuizGenerationProvider)
		if !ok {
			writeQuizError(w, 503, "quiz-unavailable")
			return
		}
		status, err := provider.ReadQuizGeneration(prID, generationID)
		if err != nil {
			writeQuizFailure(w, err)
			return
		}
		if status.Validate(prID) != nil || status.GenerationID != generationID {
			writeQuizError(w, 500, "invalid-result")
			return
		}
		writeQuizJSON(w, 200, status, quiz.MaxStatusBytes)
		return
	}
	provider, ok := s.prs.(QuizReadProvider)
	if !ok {
		writeQuizError(w, 503, "quiz-unavailable")
		return
	}
	result, err := provider.ReadQuizView(prID)
	if err != nil {
		writeQuizFailure(w, err)
		return
	}
	if result.Validate(prID) != nil {
		writeQuizError(w, 500, "invalid-result")
		return
	}
	writeQuizJSON(w, 200, result, quiz.MaxReadBytes)
}

// generateQuiz admits explicit revision-pinned work once; a 202 is not saved completion.
func (s *Server) generateQuiz(w http.ResponseWriter, r *http.Request) {
	if !s.quizRoute(w, r) {
		return
	}
	var input struct {
		Revision        githubpr.Revision `json:"revision"`
		ExpectedVersion string            `json:"expectedVersion"`
	}
	if !quizBody(w, r, 4<<10, &input) {
		return
	}
	prID := r.PathValue("id")
	expected, err := quiz.ParseVersion(input.ExpectedVersion)
	if err != nil || input.Revision.Validate(prID) != nil {
		writeQuizError(w, 400, "invalid-request")
		return
	}
	provider, ok := s.prs.(QuizGenerationProvider)
	if !ok {
		writeQuizError(w, 503, "quiz-unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	status, err := provider.GenerateQuiz(ctx, prID, input.Revision, expected)
	if err != nil {
		writeQuizFailure(w, err)
		return
	}
	id, _ := input.Revision.ID()
	if status.Validate(prID) != nil || status.RevisionID != id || status.State != quiz.Pending || status.CancelRequested || status.Sequence != "1" {
		writeQuizError(w, 500, "invalid-result")
		return
	}
	writeQuizJSON(w, 202, status, quiz.MaxStatusBytes)
}

// cancelQuiz signals an exact admitted attempt without asserting native cancellation or rollback.
func (s *Server) cancelQuiz(w http.ResponseWriter, r *http.Request) {
	if !s.quizRoute(w, r) {
		return
	}
	var input struct {
		RevisionID string `json:"revisionID"`
	}
	if !quizBody(w, r, 1<<10, &input) {
		return
	}
	if !githubpr.ValidID(input.RevisionID) {
		writeQuizError(w, 400, "invalid-request")
		return
	}
	provider, ok := s.prs.(QuizGenerationProvider)
	if !ok {
		writeQuizError(w, 503, "quiz-unavailable")
		return
	}
	status, err := provider.CancelQuizGeneration(r.PathValue("id"), r.PathValue("generation"), input.RevisionID)
	if err != nil {
		writeQuizFailure(w, err)
		return
	}
	if status.Validate(r.PathValue("id")) != nil || status.GenerationID != r.PathValue("generation") || status.RevisionID != input.RevisionID {
		writeQuizError(w, 500, "invalid-result")
		return
	}
	writeQuizJSON(w, 202, status, quiz.MaxStatusBytes)
}

// checkQuiz reveals only the explicitly checked saved question, without inference or attempt writes.
func (s *Server) checkQuiz(w http.ResponseWriter, r *http.Request) {
	if !s.quizRoute(w, r) {
		return
	}
	var input struct {
		RevisionID string `json:"revisionID"`
		ChoiceID   string `json:"choiceID"`
	}
	if !quizBody(w, r, 1<<10, &input) {
		return
	}
	if !githubpr.ValidID(input.RevisionID) || !quiz.ValidChoiceID(input.ChoiceID) {
		writeQuizError(w, 400, "invalid-request")
		return
	}
	provider, ok := s.prs.(QuizReadProvider)
	if !ok {
		writeQuizError(w, 503, "quiz-unavailable")
		return
	}
	prID := r.PathValue("id")
	result, err := provider.CheckQuizAnswer(prID, r.PathValue("quiz"), r.PathValue("question"), input.RevisionID, input.ChoiceID)
	if err != nil {
		writeQuizFailure(w, err)
		return
	}
	if result.Validate(prID) != nil || result.QuizID != r.PathValue("quiz") || result.QuestionID != r.PathValue("question") || result.RevisionID != input.RevisionID || result.ChoiceID != input.ChoiceID {
		writeQuizError(w, 500, "invalid-result")
		return
	}
	writeQuizJSON(w, 200, result, quiz.MaxCheckBytes)
}

// writeQuizJSON bounds finite encoded results before writing any successful response headers.
func writeQuizJSON(w http.ResponseWriter, status int, value any, limit int) {
	data, err := json.Marshal(value)
	if err != nil || len(data)+1 > limit {
		writeQuizError(w, 500, "result-too-large")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(data, '\n'))
}

// writeQuizFailure maps known failures to fixed public codes without native/storage diagnostics.
func writeQuizFailure(w http.ResponseWriter, err error) {
	status, code := 500, "quiz-storage-failed"
	switch {
	case errors.Is(err, quiz.ErrInvalid):
		status, code = 400, "invalid-request"
	case errors.Is(err, quiz.ErrNotFound):
		status, code = 404, "quiz-not-found"
	case errors.Is(err, quiz.ErrConflict):
		status, code = 409, "quiz-changed"
	case errors.Is(err, quiz.ErrBusy):
		status, code = 409, "quiz-busy"
	case errors.Is(err, quiz.ErrUnknown):
		status, code = 409, "execution-uncertain"
	case errors.Is(err, quiz.ErrSource):
		status, code = 410, "source-unavailable"
	case errors.Is(err, quiz.ErrLimit):
		status, code = 422, "quiz-limit"
	case errors.Is(err, quiz.ErrSaveUncertain):
		code = "save-uncertain"
	case errors.Is(err, quiz.ErrClosed):
		status, code = 503, "quiz-unavailable"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = 503, "request-timeout"
	}
	writeQuizError(w, status, code)
}

// writeQuizError emits only application-owned feedback, never request or provider text.
func writeQuizError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"code": code, "error": "Quiz request could not be completed. Read saved state before retrying uncertain work."})
}

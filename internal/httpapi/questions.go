package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// QuestionProvider owns admission, app-lifetime execution and normalized answer observation.
type QuestionProvider interface {
	AskQuestion(context.Context, string, question.InitialRequest) (question.Answer, error)
	AskFollowup(context.Context, string, string, question.FollowupRequest) (question.Answer, error)
	ObserveAnswer(context.Context, string, string, string, question.AnswerRequest, func(question.Answer) error) error
	StopAnswer(string, string, string, question.AnswerRequest) (question.Answer, error)
}

// questionProvider verifies canonical routes and registered membership before any question work.
func (s *Server) questionProvider(w http.ResponseWriter, r *http.Request) (QuestionProvider, bool) {
	if r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path || !githubpr.ValidID(r.PathValue("id")) {
		http.NotFound(w, r)
		return nil, false
	}
	for _, key := range []string{"thread", "answer"} {
		if value := r.PathValue(key); value != "" && !githubpr.ValidID(value) {
			http.NotFound(w, r)
			return nil, false
		}
	}
	if s.prs == nil {
		writeError(w, 503, "question registry unavailable")
		return nil, false
	}
	pr, err := s.prs.ReadPR(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, 404, "PR is not registered")
		return nil, false
	}
	if err != nil || pr.Validate() != nil || pr.ID != r.PathValue("id") {
		writeError(w, 500, "cannot read registered PR")
		return nil, false
	}
	provider, ok := s.prs.(QuestionProvider)
	if !ok {
		writeError(w, 503, "questions unavailable")
		return nil, false
	}
	return provider, true
}

// questionBody reads one bounded UTF-8 JSON object without tolerating extra fields or trailing values.
func questionBody(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "JSON body required")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, 413, "question body too large")
		} else {
			writeError(w, 400, "invalid question body")
		}
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(data) || decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, 400, "invalid question JSON")
		return false
	}
	return true
}

// askQuestion admits explicit initial/follow-up work; observing an answer never invokes this handler.
func (s *Server) askQuestion(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.questionProvider(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(100 * time.Second)); err != nil {
		writeError(w, 500, "cannot start question response")
		return
	}
	var answer question.Answer
	var err error
	var revisionID string
	if r.PathValue("thread") == "" {
		var input question.InitialRequest
		if !questionBody(w, r, 24<<10, &input) {
			return
		}
		if input.Validate(r.PathValue("id"), s.prs) != nil {
			writeError(w, 400, "invalid question selection, revision or text")
			return
		}
		revisionID, _ = input.Revision.ID()
		answer, err = provider.AskQuestion(ctx, r.PathValue("id"), input)
	} else {
		var input question.FollowupRequest
		if !questionBody(w, r, 24<<10, &input) {
			return
		}
		if !githubpr.ValidID(input.RevisionID) || !question.ValidText(input.Question) {
			writeError(w, 400, "invalid follow-up revision or text")
			return
		}
		revisionID = input.RevisionID
		answer, err = provider.AskFollowup(ctx, r.PathValue("id"), r.PathValue("thread"), input)
	}
	if err != nil {
		writeQuestionError(w, err)
		return
	}
	if !validAnswer(answer, r) || answer.RevisionID != revisionID {
		writeError(w, 500, "invalid question admission result")
		return
	}
	writeJSON(w, http.StatusAccepted, answer)
}

// answerEvents streams cumulative snapshots with bounded writes and no native-dispatch side effect.
func (s *Server) answerEvents(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.questionProvider(w, r)
	if !ok {
		return
	}
	var input question.AnswerRequest
	if !questionBody(w, r, 1024, &input) {
		return
	}
	if !githubpr.ValidID(input.RevisionID) {
		writeError(w, 400, "invalid answer revision")
		return
	}
	wrote := false
	err := provider.ObserveAnswer(r.Context(), r.PathValue("id"), r.PathValue("thread"), r.PathValue("answer"), input, func(answer question.Answer) error {
		if !validAnswer(answer, r) || answer.RevisionID != input.RevisionID {
			return question.ErrAnswerState
		}
		data, err := json.Marshal(answer)
		if err != nil || len(data) > 2<<20 {
			return question.ErrAnswerState
		}
		controller := http.NewResponseController(w)
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/event-stream")
		wrote = true
		if _, err := w.Write(append(append([]byte("event: answer\ndata: "), data...), '\n', '\n')); err != nil {
			return err
		}
		return controller.Flush()
	})
	if err != nil && !wrote {
		writeQuestionError(w, err)
	}
}

// stopAnswer records an exact answer's Stop request, not an assertion of successful cancellation.
func (s *Server) stopAnswer(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.questionProvider(w, r)
	if !ok {
		return
	}
	var input question.AnswerRequest
	if !questionBody(w, r, 1024, &input) {
		return
	}
	if !githubpr.ValidID(input.RevisionID) {
		writeError(w, 400, "invalid answer revision")
		return
	}
	answer, err := provider.StopAnswer(r.PathValue("id"), r.PathValue("thread"), r.PathValue("answer"), input)
	if err != nil {
		writeQuestionError(w, err)
		return
	}
	if !validAnswer(answer, r) || answer.RevisionID != input.RevisionID {
		writeError(w, 500, "invalid Stop result")
		return
	}
	writeJSON(w, http.StatusAccepted, answer)
}

// validAnswer checks public scope and bounds before any provider data is serialized.
func validAnswer(answer question.Answer, r *http.Request) bool {
	if answer.PRID != r.PathValue("id") || !githubpr.ValidID(answer.ThreadID) || !githubpr.ValidID(answer.ID) || !githubpr.ValidID(answer.RevisionID) || len(answer.Text) > 256<<10 || !utf8.ValidString(answer.Text) || !question.ValidText(answer.Question) {
		return false
	}
	if thread := r.PathValue("thread"); thread != "" && thread != answer.ThreadID {
		return false
	}
	if id := r.PathValue("answer"); id != "" && id != answer.ID {
		return false
	}
	if answer.Reference.RevisionID != answer.RevisionID || answer.Reference.Anchor.Validate() != nil {
		return false
	}
	switch answer.FailureCode {
	case "", "source-unavailable", "execution-failed", "answer-too-large", "execution-uncertain":
	default:
		return false
	}
	if answer.State == question.Completed && (answer.Text == "" || answer.ExecutionUnknown) {
		return false
	}
	switch answer.State {
	case question.Pending, question.Streaming, question.Completed, question.Failed, question.Interrupted:
		return true
	default:
		return false
	}
}

// writeQuestionError exposes fixed actionable categories without native bodies, paths or credentials.
func writeQuestionError(w http.ResponseWriter, err error) {
	status, message := 502, "question execution unavailable; no automatic retry was performed"
	switch {
	case errors.Is(err, question.ErrBusy):
		status, message = 409, question.ErrBusy.Error()
	case errors.Is(err, question.ErrUncertain):
		status, message = 409, question.ErrUncertain.Error()
	case errors.Is(err, question.ErrCapacity), errors.Is(err, question.ErrPromptLimit):
		status, message = 422, "question exceeds runtime or prompt limits; existing history was preserved"
	case errors.Is(err, question.ErrContextLimit):
		status, message = 422, "question source exceeds excerpt limits; no selection was silently truncated"
	case errors.Is(err, git.ErrQuestionSourceUnavailable):
		status, message = 410, "original pinned source is unavailable; no newer source was substituted"
	case errors.Is(err, question.ErrInvalidSelection):
		status, message = 400, "selection does not match the pinned text diff"
	case errors.Is(err, question.ErrSessionUnavailable), errors.Is(err, question.ErrRuntimeClosed):
		status, message = 503, "original question runtime/session is unavailable; no replacement was created"
	case errors.Is(err, question.ErrNotFound):
		status, message = 404, "question thread or answer not found in this PR revision"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = 504, "question request cancelled or timed out; delivery may be uncertain, do not retry automatically"
	}
	writeError(w, status, message)
}

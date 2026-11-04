package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// QuestionHistoryProvider reads saved state without requiring a source cache or native runtime.
type QuestionHistoryProvider interface {
	ListThreads(string) (question.ThreadList, error)
	ReadThread(string, string) (question.ThreadDetail, error)
}

// questionHistory serves explicit bounded reads; HEAD cannot trigger restart-classification writes.
func (s *Server) questionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "GET required")
		return
	}
	if !s.questionRoute(w, r) {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeError(w, 400, "saved history reads do not accept a body")
		return
	}
	provider, ok := s.prs.(QuestionHistoryProvider)
	if !ok {
		writeError(w, 503, "saved question history unavailable")
		return
	}
	var value any
	var err error
	limit := 2 << 20
	if r.PathValue("thread") == "" {
		var list question.ThreadList
		list, err = provider.ListThreads(r.PathValue("id"))
		if err == nil && !validThreadList(list, r.PathValue("id")) {
			err = errors.New("invalid saved list")
		}
		value = list
	} else {
		var detail question.ThreadDetail
		detail, err = provider.ReadThread(r.PathValue("id"), r.PathValue("thread"))
		if err == nil && !validThreadDetail(detail, r) {
			err = errors.New("invalid saved detail")
		}
		value, limit = detail, 8<<20
	}
	if errors.Is(err, question.ErrNotFound) {
		writeError(w, 404, "saved question thread not found")
		return
	}
	if err != nil {
		writeError(w, 500, "cannot read saved question history; existing data was preserved")
		return
	}
	data, err := json.Marshal(value)
	if err != nil || len(data)+1 > limit {
		writeError(w, 500, "saved question history exceeds response limits")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(append(data, '\n'))
}

// validHistoryVersion recognizes lossless canonical uint64 generations, optionally allowing an empty store.
func validHistoryVersion(value string, empty bool) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && strconv.FormatUint(n, 10) == value && (n != 0 || empty)
}

// validPersistence recognizes only normalized public disk-saving states.
func validPersistence(value string) bool {
	return value == "saved" || value == "saving" || value == "failed"
}

// validThreadList checks public scope, ordering identities and bounded navigation fields.
func validThreadList(list question.ThreadList, prID string) bool {
	if list.PRID != prID || !validHistoryVersion(list.Version, true) || list.Threads == nil || len(list.Threads) > 128 || list.Version == "0" && len(list.Threads) != 0 {
		return false
	}
	ids := make(map[string]bool)
	count := 0
	for _, thread := range list.Threads {
		if !githubpr.ValidID(thread.ID) || ids[thread.ID] || !githubpr.ValidID(thread.RevisionID) || !question.ValidText(thread.Question) || len(thread.Question) > 256 || thread.Anchor.Validate() != nil || thread.AnswerCount < 1 || thread.AnswerCount > 512 || !validPersistence(thread.Persistence) {
			return false
		}
		if !utf8.ValidString(thread.OldPath) || !utf8.ValidString(thread.NewPath) || strings.ContainsRune(thread.OldPath+thread.NewPath, 0) || len(thread.OldPath)+len(thread.NewPath) > question.MaxContextBytes {
			return false
		}
		count += thread.AnswerCount
		if count > 512 {
			return false
		}
		ids[thread.ID] = true
		switch thread.State {
		case question.Pending, question.Streaming, question.Completed, question.Failed, question.Interrupted:
		default:
			return false
		}
		if thread.ExecutionUnknown && thread.State != question.Interrupted {
			return false
		}
	}
	return true
}

// validThreadDetail checks saved transcript membership and public context without leaking private records.
func validThreadDetail(detail question.ThreadDetail, r *http.Request) bool {
	if detail.PRID != r.PathValue("id") || detail.ID != r.PathValue("thread") || !githubpr.ValidID(detail.RevisionID) || !validHistoryVersion(detail.Version, false) || !validPersistence(detail.Persistence) || len(detail.Answers) == 0 || len(detail.Answers) > 512 || detail.Context.Directory != "" {
		return false
	}
	id, err := detail.Context.Revision.ID()
	if err != nil || detail.Context.Revision.Validate(detail.PRID) != nil || id != detail.RevisionID || detail.Context.RevisionID != id || detail.Context.ValidateSaved() != nil {
		return false
	}
	allowed := map[string][]string{"unverified": {"continuation-requires-verification"}, "busy": {"question-active"}, "unknown": {"execution-uncertain"}, "unavailable": {"incompatible-binding", "runtime-closed", "original-session-unavailable", "answer-not-saved"}, "ready": {"original-session-verified"}}
	statusOK := false
	for _, reason := range allowed[detail.Session.State] {
		statusOK = statusOK || detail.Session.Reason == reason
	}
	if !statusOK {
		return false
	}
	ids := make(map[string]bool)
	for n, answer := range detail.Answers {
		if !validAnswer(answer, r) || answer.ID == detail.ID || answer.RevisionID != id || ids[answer.ID] || answer.Persistence != "" || answer.SavedVersion != "" || answer.Reference != (question.Reference{RevisionID: id, Anchor: detail.Context.Anchor, OldPath: detail.Context.OldPath, NewPath: detail.Context.NewPath}) {
			return false
		}
		ids[answer.ID] = true
		if answer.ExecutionUnknown && answer.State != question.Interrupted || (answer.State == question.Pending || answer.State == question.Streaming) && n != len(detail.Answers)-1 {
			return false
		}
	}
	return true
}

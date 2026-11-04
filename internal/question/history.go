package question

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SessionStatus describes current runtime evidence, never a persisted authorization to continue.
type SessionStatus struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ThreadSummary contains only bounded saved-thread navigation and last saved answer presentation.
type ThreadSummary struct {
	ID               string `json:"threadID"`
	RevisionID       string `json:"revisionID"`
	Question         string `json:"question"`
	OldPath          string `json:"oldPath,omitempty"`
	NewPath          string `json:"newPath,omitempty"`
	Anchor           Anchor `json:"anchor"`
	AnswerCount      int    `json:"answerCount"`
	State            State  `json:"state"`
	ExecutionUnknown bool   `json:"executionUnknown"`
	Persistence      string `json:"persistence"`
}

// ThreadList exposes saved creation order and a lossless file generation without native bindings.
type ThreadList struct {
	PRID    string          `json:"prID"`
	Version string          `json:"version"`
	Threads []ThreadSummary `json:"threads"`
}

// ThreadDetail exposes saved text and quotes; Persistence separately describes newer unsaved runtime work.
type ThreadDetail struct {
	PRID        string        `json:"prID"`
	Version     string        `json:"version"`
	ID          string        `json:"threadID"`
	RevisionID  string        `json:"revisionID"`
	Context     Context       `json:"context"`
	Answers     []Answer      `json:"answers"`
	Persistence string        `json:"persistence"`
	Session     SessionStatus `json:"session"`
}

// PublicList projects only saved navigation fields, preserving byte-bounded UTF-8 question excerpts.
func (s Saved) PublicList() ThreadList {
	list := ThreadList{PRID: s.PRID, Version: strconv.FormatUint(s.Version, 10), Threads: []ThreadSummary{}}
	for _, thread := range s.Threads {
		text := strings.TrimSpace(thread.Answers[0].Question)
		if len(text) > 256 {
			text = text[:256]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		latest := thread.Answers[len(thread.Answers)-1]
		list.Threads = append(list.Threads, ThreadSummary{ID: thread.ID, RevisionID: thread.RevisionID, Question: text, OldPath: thread.Context.OldPath, NewPath: thread.Context.NewPath, Anchor: thread.Context.Anchor, AnswerCount: len(thread.Answers), State: latest.State, ExecutionUnknown: latest.ExecutionUnknown, Persistence: "saved"})
	}
	return list
}

// PublicThread returns detached saved transcript fields without host paths or native identifiers.
func (s Saved) PublicThread(threadID string) (ThreadDetail, error) {
	for _, thread := range s.Threads {
		if thread.ID != threadID {
			continue
		}
		context := thread.Context
		context.Directory = ""
		context.Selected = append(context.Selected[:0:0], context.Selected...)
		context.Surrounding.Lines = append(context.Surrounding.Lines[:0:0], context.Surrounding.Lines...)
		status := SessionStatus{State: "unverified", Reason: "continuation-requires-verification"}
		for _, binding := range s.Bindings {
			if binding.RevisionID == thread.RevisionID && (binding.State != "bound" || binding.Native == nil || binding.Native.Validate() != nil) {
				status = SessionStatus{State: "unavailable", Reason: "incompatible-binding"}
			}
		}
		return ThreadDetail{PRID: s.PRID, Version: strconv.FormatUint(s.Version, 10), ID: thread.ID, RevisionID: thread.RevisionID, Context: context, Answers: append([]Answer(nil), thread.Answers...), Persistence: "saved", Session: status}, nil
	}
	return ThreadDetail{}, ErrNotFound
}

// HistoryStatus reports existing runtime evidence only; it never verifies source or starts native work.
func (r *Runtime) HistoryStatus(prID, revisionID, threadID string) (string, SessionStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	persistence := "saved"
	if thread := r.threads[threadID]; thread != nil && thread.context.Revision.PRID == prID {
		for _, turn := range thread.answers {
			if turn.saveState == "failed" {
				persistence = "failed"
				break
			}
			if turn.saveState == "saving" {
				persistence = "saving"
			}
		}
	}
	status := SessionStatus{State: "unverified", Reason: "continuation-requires-verification"}
	b := r.bindings[revisionKey{prID, revisionID}]
	switch {
	case r.closed:
		status = SessionStatus{State: "unavailable", Reason: "runtime-closed"}
	case b == nil:
	case errors.Is(b.broken, ErrUncertain):
		status = SessionStatus{State: "unknown", Reason: "execution-uncertain"}
	case b.broken != nil || b.session != nil && (r.service == nil || r.service.Err() != nil):
		status = SessionStatus{State: "unavailable", Reason: "original-session-unavailable"}
	case b.saveFailed:
		status = SessionStatus{State: "unavailable", Reason: "answer-not-saved"}
	case b.busy:
		status = SessionStatus{State: "busy", Reason: "question-active"}
	case b.session != nil:
		status = SessionStatus{State: "ready", Reason: "original-session-verified"}
	}
	return persistence, status
}

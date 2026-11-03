// Package question validates routed questions and pinned source, not native execution or persistence.
package question

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// MaxQuestionBytes bounds UTF-8 question text independently of HTTP framing.
const MaxQuestionBytes = 16 << 10

// Registry looks up existing PRs; validation never registers a new identity.
type Registry interface {
	ReadPR(string) (githubpr.PullRequest, error)
}

// Anchor selects one side's lines within a hunk, or the entire unified hunk.
// References and coordinates require verification against pinned source before dispatch.
type Anchor struct {
	Kind   string `json:"kind"`
	FileID string `json:"fileID"`
	HunkID string `json:"hunkID"`
	Side   string `json:"side,omitempty"`
	Start  int    `json:"start,omitempty"`
	End    int    `json:"end,omitempty"`
}

// Validate checks supported anchor shapes, not source existence or non-text eligibility.
func (a Anchor) Validate() error {
	if !githubpr.ValidID(a.FileID) || !githubpr.ValidID(a.HunkID) {
		return errors.New("invalid question source references")
	}
	switch a.Kind {
	case "lines":
		if (a.Side == "old" || a.Side == "new") && a.Start > 0 && a.End >= a.Start && a.End <= 1_000_100_000 && a.End-a.Start < 1000 {
			return nil
		}
	case "hunk":
		if a.Side == "" && a.Start == 0 && a.End == 0 {
			return nil
		}
	}
	return errors.New("unsupported question anchor")
}

// InitialRequest supplies pinned comparison identity and a selection, never quoted source.
type InitialRequest struct {
	Revision githubpr.Revision `json:"revision"`
	Anchor   Anchor            `json:"anchor"`
	Question string            `json:"question"`
}

// Validate checks registered PR membership and bounded input before source resolution.
func (r InitialRequest) Validate(prID string, registry Registry) error {
	if err := registered(prID, registry); err != nil {
		return err
	}
	if err := r.Revision.Validate(prID); err != nil {
		return errors.New("invalid question revision")
	}
	if err := r.Anchor.Validate(); err != nil {
		return err
	}
	return validateText(r.Question)
}

// ThreadContext is an authoritative runtime lookup result, not browser input or native state.
type ThreadContext struct {
	ID       string
	Revision githubpr.Revision
}

// FollowupRequest continues the routed thread's original comparison.
type FollowupRequest struct {
	RevisionID string `json:"revisionID"`
	Question   string `json:"question"`
}

// Validate checks the requested scope against a previously looked-up runtime thread.
func (r FollowupRequest) Validate(prID, threadID string, registry Registry, thread ThreadContext) error {
	if err := validateThread(prID, threadID, r.RevisionID, registry, thread); err != nil {
		return err
	}
	return validateText(r.Question)
}

// AnswerContext identifies a looked-up runtime answer independently of native message IDs.
type AnswerContext struct {
	ID     string
	Thread ThreadContext
}

// AnswerRequest scopes answer observation or Stop to an exact revision and routed answer.
type AnswerRequest struct {
	RevisionID string `json:"revisionID"`
}

// Validate checks stream/Stop scope; it neither interrupts nor certifies execution state.
func (r AnswerRequest) Validate(prID, threadID, answerID string, registry Registry, answer AnswerContext) error {
	if err := validateThread(prID, threadID, r.RevisionID, registry, answer.Thread); err != nil {
		return err
	}
	if !githubpr.ValidID(answerID) || answer.ID != answerID {
		return errors.New("unknown question answer")
	}
	return nil
}

// registered checks canonical identity and registry membership without returning raw storage errors.
func registered(prID string, registry Registry) error {
	if !githubpr.ValidID(prID) {
		return errors.New("invalid question pull request identity")
	}
	if registry == nil {
		return errors.New("question PR registry unavailable")
	}
	pr, err := registry.ReadPR(prID)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("question PR is not registered")
	}
	if err != nil || pr.ID != prID || pr.Validate() != nil {
		return errors.New("cannot read question PR")
	}
	return nil
}

// validateThread checks runtime membership and the original canonical comparison identity.
func validateThread(prID, threadID, revisionID string, registry Registry, thread ThreadContext) error {
	if err := registered(prID, registry); err != nil {
		return err
	}
	if !githubpr.ValidID(threadID) || thread.ID != threadID || thread.Revision.Validate(prID) != nil {
		return errors.New("unknown question thread")
	}
	expected, err := thread.Revision.ID()
	if err != nil || revisionID != expected {
		return errors.New("question revision does not match thread")
	}
	return nil
}

// validateText preserves literal nonblank text while bounding its UTF-8 representation.
func validateText(text string) error {
	if len(text) > MaxQuestionBytes || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return errors.New("question must be nonblank UTF-8 text within 16384 bytes without NUL")
	}
	return nil
}

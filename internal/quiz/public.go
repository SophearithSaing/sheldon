package quiz

import (
	"errors"
	"reflect"
	"strconv"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

const (
	MaxReadBytes   = 256 << 10
	MaxCheckBytes  = 512 << 10
	MaxStatusBytes = 4 << 10
)

var ErrInvalid = errors.New("invalid quiz choice, version or request")

// PublicQuestion contains no key, explanation, evidence or correctness-derived metadata.
type PublicQuestion struct {
	ID      string   `json:"questionID"`
	Prompt  string   `json:"prompt"`
	Choices []Choice `json:"choices"`
}

// PublicQuiz is the unanswered projection of one immutable saved set.
type PublicQuiz struct {
	ID         string            `json:"quizID"`
	Revision   githubpr.Revision `json:"revision"`
	RevisionID string            `json:"revisionID"`
	Questions  []PublicQuestion  `json:"questions"`
}

// ReadResult separates saved bytes from current runtime feedback without leaking private records.
type ReadResult struct {
	PRID       string      `json:"prID"`
	Version    string      `json:"version"`
	Quiz       *PublicQuiz `json:"quiz"`
	Generation *Status     `json:"generation"`
}

// Checked reveals only the explicitly checked question's key and evidence; no attempt is saved.
type Checked struct {
	PRID            string     `json:"prID"`
	Version         string     `json:"version"`
	QuizID          string     `json:"quizID"`
	RevisionID      string     `json:"revisionID"`
	QuestionID      string     `json:"questionID"`
	ChoiceID        string     `json:"choiceID"`
	Correct         bool       `json:"correct"`
	CorrectChoiceID string     `json:"correctChoiceID"`
	Explanation     string     `json:"explanation"`
	Evidence        []Evidence `json:"evidence"`
}

// ParseVersion accepts lossless canonical decimal uint64 versions, including an empty store's zero.
func ParseVersion(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, ErrInvalid
	}
	return n, nil
}

// ValidQuestionID recognizes IDs scoped to one exact quiz set, never a global question identity.
func ValidQuestionID(value string) bool {
	return len(value) == 2 && value[0] == 'q' && value[1] >= '1' && value[1] <= '5'
}

// ValidChoiceID recognizes one of the four ordered alternatives.
func ValidChoiceID(value string) bool { return len(value) == 1 && value[0] >= 'a' && value[0] <= 'd' }

// Public projects validated saved data without retaining private question structs or aliases.
func (s Saved) Public() (ReadResult, error) {
	result := ReadResult{PRID: s.PRID, Version: strconv.FormatUint(s.Version, 10)}
	if s.Version == 0 {
		if !githubpr.ValidID(s.PRID) || !reflect.DeepEqual(s, EmptySaved(s.PRID)) {
			return ReadResult{}, ErrStore
		}
		return result, nil
	}
	if _, err := s.Encode(); err != nil {
		return ReadResult{}, err
	}
	view := &PublicQuiz{ID: s.QuizID, Revision: s.Revision, RevisionID: s.RevisionID, Questions: make([]PublicQuestion, 0, 5)}
	for _, item := range s.Questions {
		view.Questions = append(view.Questions, PublicQuestion{ID: item.ID, Prompt: item.Prompt, Choices: append([]Choice(nil), item.Choices...)})
	}
	result.Quiz = view
	return result, nil
}

// Check grades only an identified saved set and copies one question's checked evidence.
func (s Saved) Check(quizID, questionID, revisionID, choiceID string) (Checked, error) {
	if !githubpr.ValidID(quizID) || !githubpr.ValidID(revisionID) || !ValidChoiceID(choiceID) {
		return Checked{}, ErrInvalid
	}
	if !ValidQuestionID(questionID) {
		return Checked{}, ErrNotFound
	}
	if s.Version == 0 {
		if !reflect.DeepEqual(s, EmptySaved(s.PRID)) || !githubpr.ValidID(s.PRID) {
			return Checked{}, ErrStore
		}
		return Checked{}, ErrNotFound
	}
	if _, err := s.Encode(); err != nil {
		return Checked{}, err
	}
	if s.QuizID != quizID || s.RevisionID != revisionID {
		return Checked{}, ErrConflict
	}
	item := s.Questions[int(questionID[1]-'1')]
	checked := Checked{PRID: s.PRID, Version: strconv.FormatUint(s.Version, 10), QuizID: s.QuizID, RevisionID: s.RevisionID, QuestionID: item.ID, ChoiceID: choiceID, Correct: choiceID == item.CorrectChoiceID, CorrectChoiceID: item.CorrectChoiceID, Explanation: item.Explanation, Evidence: make([]Evidence, 0, len(item.Evidence))}
	for _, e := range item.Evidence {
		e.Selected = append([]git.DiffLine(nil), e.Selected...)
		if e.Surrounding != nil {
			hunk := *e.Surrounding
			hunk.Lines = append([]git.DiffLine(nil), hunk.Lines...)
			e.Surrounding = &hunk
		}
		checked.Evidence = append(checked.Evidence, e)
	}
	return checked, nil
}

// Validate checks public-only set/version/status structure without source or native work.
func (r ReadResult) Validate(prID string) error {
	n, err := ParseVersion(r.Version)
	if err != nil || !githubpr.ValidID(prID) || r.PRID != prID || (r.Quiz == nil) != (n == 0) {
		return ErrOutput
	}
	if r.Generation != nil && r.Generation.Validate(prID) != nil {
		return ErrOutput
	}
	if r.Quiz == nil {
		return nil
	}
	q := r.Quiz
	id, err := q.Revision.ID()
	if err != nil || q.Revision.Validate(prID) != nil || q.RevisionID != id || !githubpr.ValidID(q.ID) || len(q.Questions) != 5 {
		return ErrOutput
	}
	prompts := []string{}
	for i, item := range q.Questions {
		if item.ID != "q"+string(rune('1'+i)) || !validText(item.Prompt, maxPromptText) || duplicateText(prompts, item.Prompt) || len(item.Choices) != 4 {
			return ErrOutput
		}
		prompts = append(prompts, item.Prompt)
		texts := []string{}
		for j, c := range item.Choices {
			if c.ID != string(rune('a'+j)) || !validText(c.Text, maxChoiceText) || duplicateText(texts, c.Text) {
				return ErrOutput
			}
			texts = append(texts, c.Text)
		}
	}
	return nil
}

// Validate checks one checked projection and bounded literal evidence without consulting Git.
func (c Checked) Validate(prID string) error {
	n, err := ParseVersion(c.Version)
	if err != nil || n == 0 || !githubpr.ValidID(prID) || c.PRID != prID || !githubpr.ValidID(c.QuizID) || !githubpr.ValidID(c.RevisionID) || !ValidQuestionID(c.QuestionID) || !ValidChoiceID(c.ChoiceID) || !ValidChoiceID(c.CorrectChoiceID) || c.Correct != (c.ChoiceID == c.CorrectChoiceID) || !validText(c.Explanation, maxExplanation) || len(c.Evidence) < 1 || len(c.Evidence) > 2 {
		return ErrOutput
	}
	for i, e := range c.Evidence {
		if validateEvidence(e) != nil || i > 0 && e.Anchor == c.Evidence[0].Anchor {
			return ErrOutput
		}
	}
	return nil
}

// Validate permits only scoped bounded runtime states and fixed feedback, never raw failures.
func (s Status) Validate(prID string) error {
	seq, err := ParseVersion(s.Sequence)
	if err != nil || seq == 0 || !githubpr.ValidID(prID) || s.PRID != prID || !githubpr.ValidID(s.GenerationID) || !githubpr.ValidID(s.QuizID) || s.GenerationID == s.QuizID || !githubpr.ValidID(s.RevisionID) || s.ExecutionUnknown != (s.State == Unknown) {
		return ErrOutput
	}
	if s.State == Completed {
		version, err := ParseVersion(s.SavedVersion)
		if err != nil || version == 0 || s.CancelRequested || s.FailureCode != "" {
			return ErrOutput
		}
	} else if s.SavedVersion != "" {
		return ErrOutput
	}
	switch s.State {
	case Pending, Generating, Validating, Saving, Completed, Cancelled:
		if s.FailureCode != "" {
			return ErrOutput
		}
	case Unknown:
		if s.FailureCode != "session-uncertain" && s.FailureCode != "execution-uncertain" {
			return ErrOutput
		}
	case Failed:
		switch s.FailureCode {
		case "source-unavailable", "source-too-large", "generation-timeout", "native-unavailable", "output-too-large", "execution-failed", "invalid-output", "save-failed", "quiz-changed", "save-uncertain":
		default:
			return ErrOutput
		}
	default:
		return ErrOutput
	}
	return nil
}

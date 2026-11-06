package quiz

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

var (
	ErrStore         = errors.New("cannot read or save private quiz storage; preserve existing data")
	ErrConflict      = errors.New("saved quiz changed; read it before generating again")
	ErrSaveUncertain = errors.New("quiz replacement is uncertain; explicitly read saved quiz before another mutation")
)

// Persistence checks admission health and atomically replaces one complete saved set.
type Persistence interface {
	CheckQuiz(string, uint64) error
	SaveQuiz(Saved, uint64) error
}

// Saved is a private keyed set, never an unanswered public response or an attempt record.
type Saved struct {
	SchemaVersion int    `json:"schemaVersion"`
	PRID          string `json:"prID"`
	Version       uint64 `json:"version"`
	QuizID        string `json:"quizID"`
	Content
}

// EmptySaved describes genuinely absent storage without authorizing a version-zero file.
func EmptySaved(prID string) Saved { return Saved{SchemaVersion: 1, PRID: prID} }

// Encode validates a complete saved set and its final encoded budget, including newline.
func (s Saved) Encode() ([]byte, error) {
	if s.SchemaVersion != 1 || s.Version == 0 || !githubpr.ValidID(s.PRID) || !githubpr.ValidID(s.QuizID) || s.Revision.Validate(s.PRID) != nil || s.Content.validate() != nil {
		return nil, ErrStore
	}
	data, err := json.Marshal(s)
	if err != nil || len(data)+1 > MaxContentBytes {
		return nil, ErrStore
	}
	return append(data, '\n'), nil
}

// DecodeSaved reads strict bounded storage without consulting Git, models or native history.
func DecodeSaved(data []byte, prID string) (Saved, error) {
	var saved Saved
	if len(data) > MaxContentBytes || !decodeOutput(data, &saved) || saved.PRID != prID {
		return Saved{}, ErrStore
	}
	if _, err := saved.Encode(); err != nil {
		return Saved{}, err
	}
	return saved, nil
}

// validate checks retained graph/quote consistency, not source existence or factual accuracy.
func (c Content) validate() error {
	id, err := c.Revision.ID()
	if err != nil || id != c.RevisionID || len(c.Questions) != 5 {
		return ErrStore
	}
	prompts := []string{}
	paths := make(map[string][2]string)
	hunks := make(map[string]git.DiffHunk)
	total := 0
	for i, item := range c.Questions {
		if item.ID != "q"+string(rune('1'+i)) || !validText(item.Prompt, maxPromptText) || duplicateText(prompts, item.Prompt) || !validText(item.Explanation, maxExplanation) || len(item.Choices) != 4 || len(item.CorrectChoiceID) != 1 || item.CorrectChoiceID[0] < 'a' || item.CorrectChoiceID[0] > 'd' || len(item.Evidence) < 1 || len(item.Evidence) > 2 {
			return ErrStore
		}
		prompts = append(prompts, item.Prompt)
		texts := []string{}
		for j, choice := range item.Choices {
			if choice.ID != string(rune('a'+j)) || !validText(choice.Text, maxChoiceText) || duplicateText(texts, choice.Text) {
				return ErrStore
			}
			texts = append(texts, choice.Text)
		}
		for j, evidence := range item.Evidence {
			a := evidence.Anchor
			if j > 0 && a == item.Evidence[0].Anchor || a.End-a.Start >= maxEvidenceLines || len(evidence.Selected) > maxEvidenceLines {
				return ErrStore
			}
			context := question.Context{Revision: c.Revision, RevisionID: id, Anchor: question.Anchor{Kind: "lines", FileID: a.FileID, HunkID: a.HunkID, Side: a.Side, Start: a.Start, End: a.End}, OldPath: evidence.OldPath, NewPath: evidence.NewPath, Selected: evidence.Selected, Surrounding: evidence.Surrounding}
			if context.ValidateSaved() != nil {
				return ErrStore
			}
			size := evidenceBytes(evidence)
			total += size
			if size > maxEvidenceBytes || total > maxEvidenceTotal {
				return ErrStore
			}
			labels := [2]string{evidence.OldPath, evidence.NewPath}
			if previous, ok := paths[a.FileID]; ok && previous != labels {
				return ErrStore
			}
			paths[a.FileID] = labels
			key := a.FileID + a.HunkID
			if previous, ok := hunks[key]; ok && !reflect.DeepEqual(previous, evidence.Surrounding) {
				return ErrStore
			}
			hunks[key] = evidence.Surrounding
		}
	}
	return nil
}

// evidenceBytes counts literal source/path bytes, including repeated selected/surrounding quotes.
func evidenceBytes(e Evidence) int {
	size := len(e.OldPath) + len(e.NewPath) + len(e.Surrounding.Section)
	for _, lines := range [][]git.DiffLine{e.Selected, e.Surrounding.Lines} {
		for _, line := range lines {
			size += len(line.Text) + 1
		}
	}
	return size
}

// String withholds saved answer keys, explanations and original source quotes.
func (s Saved) String() string { return "saved quiz (private details withheld)" }

// GoString keeps private saved content out of debug formatting.
func (s Saved) GoString() string { return s.String() }

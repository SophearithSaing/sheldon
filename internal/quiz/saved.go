package quiz

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
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
	lines := make(map[string]git.DiffLine)
	coordinates := make(map[string]git.DiffLine)
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
			if validateEvidence(evidence) != nil {
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
			quoted := evidence.Selected
			if evidence.Surrounding != nil {
				if previous, ok := hunks[key]; ok && !reflect.DeepEqual(previous, *evidence.Surrounding) {
					return ErrStore
				}
				hunks[key] = *evidence.Surrounding
				quoted = evidence.Surrounding.Lines
			}
			for _, line := range quoted {
				lineKey := key + line.ID
				if previous, ok := lines[lineKey]; ok && previous != line {
					return ErrStore
				}
				lines[lineKey] = line
				for side, number := range map[string]int{"old": line.OldLine, "new": line.NewLine} {
					if number == 0 {
						continue
					}
					coordinate := key + side + strconv.Itoa(number)
					if previous, ok := coordinates[coordinate]; ok && previous != line {
						return ErrStore
					}
					coordinates[coordinate] = line
				}
			}
		}
	}
	return nil
}

// validateEvidence checks literal retained ranges without source access or revision substitution.
func validateEvidence(e Evidence) error {
	if err := validateSelected(e); err != nil {
		return err
	}
	if e.Surrounding == nil {
		return nil
	}
	a, h := e.Anchor, e.Surrounding
	if !githubpr.ValidID(a.FileID) || !githubpr.ValidID(a.HunkID) || a.Side != "old" && a.Side != "new" || a.Start <= 0 || a.End < a.Start || a.End > 1_000_100_000 || a.End-a.Start >= maxEvidenceLines || h.ID != a.HunkID || !githubpr.ValidID(h.Old.ID) || !githubpr.ValidID(h.New.ID) || h.Old.Start < 0 || h.New.Start < 0 || h.Old.Start > 1_000_100_000 || h.New.Start > 1_000_100_000 || h.Old.Count < 0 || h.New.Count < 0 || h.Old.Count > 100_000 || h.New.Count > 100_000 || len(h.Lines) > 100_000 || len(e.Selected) == 0 || len(e.Selected) > maxEvidenceLines || e.OldPath == "" && e.NewPath == "" || evidenceBytes(e) > maxEvidenceBytes {
		return ErrStore
	}
	for _, text := range []string{e.OldPath, e.NewPath, h.Section} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return ErrStore
		}
	}
	old, next := h.Old.Start, h.New.Start
	oldCount, newCount := 0, 0
	selected := []git.DiffLine{}
	ids := map[string]bool{}
	changed := false
	for _, line := range h.Lines {
		if !githubpr.ValidID(line.ID) || ids[line.ID] || !utf8.ValidString(line.Text) || strings.ContainsAny(line.Text, "\x00\n") {
			return ErrStore
		}
		ids[line.ID] = true
		switch line.Sign {
		case " ":
			if line.OldLine != old || line.NewLine != next || old <= 0 || next <= 0 {
				return ErrStore
			}
			old++
			next++
			oldCount++
			newCount++
		case "-":
			if line.OldLine != old || line.NewLine != 0 || old <= 0 {
				return ErrStore
			}
			old++
			oldCount++
		case "+":
			if line.NewLine != next || line.OldLine != 0 || next <= 0 {
				return ErrStore
			}
			next++
			newCount++
		default:
			return ErrStore
		}
		coordinate := line.NewLine
		if a.Side == "old" {
			coordinate = line.OldLine
		}
		if coordinate >= a.Start && coordinate <= a.End {
			selected = append(selected, line)
			changed = changed || a.Side == "new" && line.Sign == "+" || a.Side == "old" && line.Sign == "-"
		}
	}
	if !changed || oldCount != h.Old.Count || newCount != h.New.Count || old > 1_000_100_001 || next > 1_000_100_001 || len(selected) != a.End-a.Start+1 || !reflect.DeepEqual(selected, e.Selected) {
		return ErrStore
	}
	return nil
}

// validateSelected checks an exact one-sided retained quote without requiring the complete original hunk.
func validateSelected(e Evidence) error {
	a := e.Anchor
	if !githubpr.ValidID(a.FileID) || !githubpr.ValidID(a.HunkID) || a.Side != "old" && a.Side != "new" || a.Start <= 0 || a.End < a.Start || a.End > 1_000_100_000 || a.End-a.Start >= maxEvidenceLines || len(e.Selected) != a.End-a.Start+1 || e.OldPath == "" && e.NewPath == "" || evidenceBytes(e) > maxEvidenceBytes {
		return ErrStore
	}
	for _, path := range []string{e.OldPath, e.NewPath} {
		if !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
			return ErrStore
		}
	}
	ids := map[string]bool{}
	changed, previous := false, 0
	for i, line := range e.Selected {
		if !githubpr.ValidID(line.ID) || ids[line.ID] || !utf8.ValidString(line.Text) || strings.ContainsAny(line.Text, "\x00\n") {
			return ErrStore
		}
		ids[line.ID] = true
		number, other, sign := line.NewLine, line.OldLine, "+"
		if a.Side == "old" {
			number, other, sign = line.OldLine, line.NewLine, "-"
		}
		if number != a.Start+i {
			return ErrStore
		}
		switch line.Sign {
		case sign:
			if other != 0 {
				return ErrStore
			}
			changed = true
		case " ":
			if other <= previous || other > 1_000_100_000 {
				return ErrStore
			}
			previous = other
		default:
			return ErrStore
		}
	}
	if !changed {
		return ErrStore
	}
	return nil
}

// evidenceBytes counts selected/path bytes, plus surrounding context only for legacy retained evidence.
func evidenceBytes(e Evidence) int {
	size := len(e.OldPath) + len(e.NewPath)
	for _, line := range e.Selected {
		size += len(line.Text) + 1
	}
	if e.Surrounding != nil {
		size += len(e.Surrounding.Section)
		for _, line := range e.Surrounding.Lines {
			size += len(line.Text) + 1
		}
	}
	return size
}

// String withholds saved answer keys, explanations and original source quotes.
func (s Saved) String() string { return "saved quiz (private details withheld)" }

// GoString keeps private saved content out of debug formatting.
func (s Saved) GoString() string { return s.String() }

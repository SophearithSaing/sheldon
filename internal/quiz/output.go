package quiz

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

const (
	MaxOutputBytes   = 128 << 10
	MaxContentBytes  = 2 << 20
	maxPromptText    = 2 << 10
	maxChoiceText    = 1 << 10
	maxExplanation   = 4 << 10
	maxEvidenceBytes = 8 << 10
	maxEvidenceTotal = 64 << 10
	maxEvidenceLines = 200
)

// Anchor references one contiguous side of a revision-scoped changed text hunk.
type Anchor struct {
	FileID string `json:"fileID"`
	HunkID string `json:"hunkID"`
	Side   string `json:"side"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

// Evidence retains backend-derived literal quotes, not model-written source.
type Evidence struct {
	Anchor      Anchor         `json:"anchor"`
	OldPath     string         `json:"oldPath,omitempty"`
	NewPath     string         `json:"newPath,omitempty"`
	Selected    []git.DiffLine `json:"selected"`
	Surrounding git.DiffHunk   `json:"surrounding"`
}

// Choice identifies an ordered answer option within one question.
type Choice struct {
	ID   string `json:"choiceID"`
	Text string `json:"text"`
}

// Question contains a private key and explanation; it is not an unanswered public DTO.
type Question struct {
	ID              string     `json:"questionID"`
	Prompt          string     `json:"prompt"`
	Choices         []Choice   `json:"choices"`
	CorrectChoiceID string     `json:"correctChoiceID"`
	Explanation     string     `json:"explanation"`
	Evidence        []Evidence `json:"evidence"`
}

// Content is validated private generation output, not a saved quiz or admitted run.
type Content struct {
	Revision   githubpr.Revision `json:"revision"`
	RevisionID string            `json:"revisionID"`
	Questions  []Question        `json:"questions"`
}

// String prevents accidental diagnostic disclosure of source, keys and generated content.
func (c Content) String() string { return "validated quiz content (private details withheld)" }

// GoString keeps private content out of debug formatting.
func (c Content) GoString() string { return c.String() }

type nativeQuestion struct {
	Prompt          string           `json:"prompt"`
	Choices         []string         `json:"choices"`
	CorrectChoiceID string           `json:"correctChoiceID"`
	Explanation     string           `json:"explanation"`
	Evidence        []nativeEvidence `json:"evidence"`
}

type nativeEvidence struct {
	Path  string `json:"path"`
	Side  string `json:"side"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Parse validates one complete native result and derives evidence from this exact source.
// It neither certifies factual correctness nor creates public projections or durable state.
func (s *Source) Parse(data []byte) (Content, error) {
	if s == nil || s.directory == "" || s.prompt == "" || !s.diff.Complete {
		return Content{}, ErrSource
	}
	if len(data) > MaxOutputBytes {
		return Content{}, ErrLimit
	}
	var output struct {
		Questions []nativeQuestion `json:"questions"`
	}
	if !decodeOutput(data, &output) || len(output.Questions) != 5 {
		return Content{}, ErrOutput
	}
	result := Content{Revision: s.diff.Revision, RevisionID: s.diff.RevisionID, Questions: make([]Question, 0, 5)}
	prompts := []string{}
	total := 0
	for i, item := range output.Questions {
		if !validText(item.Prompt, maxPromptText) || duplicateText(prompts, item.Prompt) || !validText(item.Explanation, maxExplanation) || len(item.Choices) != 4 || len(item.CorrectChoiceID) != 1 || item.CorrectChoiceID[0] < 'a' || item.CorrectChoiceID[0] > 'd' || len(item.Evidence) < 1 || len(item.Evidence) > 2 {
			return Content{}, ErrOutput
		}
		prompts = append(prompts, item.Prompt)
		question := Question{ID: "q" + string(rune('1'+i)), Prompt: item.Prompt, CorrectChoiceID: item.CorrectChoiceID, Explanation: item.Explanation, Choices: make([]Choice, 0, 4), Evidence: make([]Evidence, 0, len(item.Evidence))}
		for j, text := range item.Choices {
			if !validText(text, maxChoiceText) || duplicateText(item.Choices[:j], text) {
				return Content{}, ErrOutput
			}
			question.Choices = append(question.Choices, Choice{ID: string(rune('a' + j)), Text: text})
		}
		for j, reference := range item.Evidence {
			if j > 0 && reference == item.Evidence[0] {
				return Content{}, ErrOutput
			}
			evidence, size, err := s.resolveEvidence(reference)
			if err != nil {
				return Content{}, err
			}
			total += size
			if total > maxEvidenceTotal {
				return Content{}, ErrLimit
			}
			question.Evidence = append(question.Evidence, evidence)
		}
		result.Questions = append(result.Questions, question)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded)+1 > MaxContentBytes {
		return Content{}, ErrLimit
	}
	return result, nil
}

// resolveEvidence maps one exact side-specific path/range to authoritative original evidence, never host access.
func (s *Source) resolveEvidence(reference nativeEvidence) (Evidence, int, error) {
	if reference.Path == "" || reference.Side != "old" && reference.Side != "new" || reference.Start <= 0 || reference.End < reference.Start || reference.End > 1_000_100_000 {
		return Evidence{}, 0, ErrOutput
	}
	if reference.End-reference.Start >= maxEvidenceLines {
		return Evidence{}, 0, ErrLimit
	}
	var resolved Evidence
	size := 0
	found := false
	for _, file := range s.diff.Files {
		path := file.NewPath
		if reference.Side == "old" {
			path = file.OldPath
		}
		if path != reference.Path || file.Kind != "text" {
			continue
		}
		for _, hunk := range file.Hunks {
			bounds := hunk.New
			if reference.Side == "old" {
				bounds = hunk.Old
			}
			if reference.Start < bounds.Start || reference.End >= bounds.Start+bounds.Count {
				continue
			}
			evidence, bytes, err := s.evidence(Anchor{FileID: file.ID, HunkID: hunk.ID, Side: reference.Side, Start: reference.Start, End: reference.End})
			if err != nil {
				return Evidence{}, 0, err
			}
			if found {
				return Evidence{}, 0, ErrOutput
			}
			resolved, size, found = evidence, bytes, true
		}
	}
	if !found {
		return Evidence{}, 0, ErrOutput
	}
	return resolved, size, nil
}

// evidence copies an eligible literal range and its complete surrounding hunk.
func (s *Source) evidence(anchor Anchor) (Evidence, int, error) {
	if !githubpr.ValidID(anchor.FileID) || !githubpr.ValidID(anchor.HunkID) || anchor.Side != "old" && anchor.Side != "new" || anchor.Start <= 0 || anchor.End < anchor.Start || anchor.End > 1_000_100_000 {
		return Evidence{}, 0, ErrOutput
	}
	if anchor.End-anchor.Start >= maxEvidenceLines {
		return Evidence{}, 0, ErrLimit
	}
	for _, file := range s.diff.Files {
		if file.ID != anchor.FileID || file.Kind != "text" {
			continue
		}
		for _, hunk := range file.Hunks {
			if hunk.ID != anchor.HunkID {
				continue
			}
			selected := []git.DiffLine{}
			changed := false
			for _, line := range hunk.Lines {
				coordinate := line.NewLine
				if anchor.Side == "old" {
					coordinate = line.OldLine
				}
				if coordinate >= anchor.Start && coordinate <= anchor.End {
					selected = append(selected, line)
					changed = changed || anchor.Side == "new" && line.Sign == "+" || anchor.Side == "old" && line.Sign == "-"
				}
			}
			if !changed || len(selected) != anchor.End-anchor.Start+1 {
				return Evidence{}, 0, ErrOutput
			}
			size := len(file.OldPath) + len(file.NewPath) + len(hunk.Section)
			for _, lines := range [][]git.DiffLine{selected, hunk.Lines} {
				for _, line := range lines {
					size += len(line.Text) + 1
				}
			}
			if size > maxEvidenceBytes {
				return Evidence{}, 0, ErrLimit
			}
			hunk.Lines = append([]git.DiffLine(nil), hunk.Lines...)
			return Evidence{Anchor: anchor, OldPath: file.OldPath, NewPath: file.NewPath, Selected: selected, Surrounding: hunk}, size, nil
		}
	}
	return Evidence{}, 0, ErrOutput
}

// validText bounds literal nonblank UTF-8 without rewriting accepted text.
func validText(text string, limit int) bool {
	return len(text) <= limit && utf8.ValidString(text) && strings.TrimSpace(text) != "" && !strings.ContainsRune(text, 0)
}

// duplicateText detects case-folded, edge-trimmed duplicates without altering stored copy.
func duplicateText(previous []string, text string) bool {
	for _, item := range previous {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(text)) {
			return true
		}
	}
	return false
}

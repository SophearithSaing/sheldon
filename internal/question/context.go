package question

import (
	"context"
	"errors"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// MaxContextBytes bounds all selected and surrounding source text without truncation.
const MaxContextBytes = 64 << 10

// ErrInvalidSelection indicates a reference or range outside the pinned changed source.
var ErrInvalidSelection = errors.New("question selection does not match the pinned text diff")

// ErrContextLimit refuses oversized selections or excerpts instead of truncating source.
var ErrContextLimit = errors.New("question source exceeds the 1000-line selection or 64 KiB context limit")

// Context contains backend-derived quotes; old coordinates refer to the merge base, not base tip.
// Surrounding preserves the complete selected hunk, including its old-version excerpt.
type Context struct {
	Revision    githubpr.Revision `json:"revision"`
	RevisionID  string            `json:"revisionID"`
	Anchor      Anchor            `json:"anchor"`
	OldPath     string            `json:"oldPath,omitempty"`
	NewPath     string            `json:"newPath,omitempty"`
	Selected    []git.DiffLine    `json:"selected"`
	Surrounding git.DiffHunk      `json:"surrounding"`
	Directory   string            `json:"-"`
}

// Resolve validates registered input and derives bounded context from existing exact source.
func Resolve(ctx context.Context, prID string, registry Registry, cache *git.Cache, input InitialRequest) (Context, error) {
	if err := input.Validate(prID, registry); err != nil {
		return Context{}, err
	}
	pr, err := registry.ReadPR(prID)
	if err != nil || pr.ID != prID || pr.Validate() != nil {
		return Context{}, errors.New("cannot read question PR")
	}
	if cache == nil {
		return Context{}, git.ErrQuestionSourceUnavailable
	}
	prepared, diff, err := cache.QuestionSource(ctx, pr, input.Revision)
	if err != nil {
		return Context{}, err
	}
	return selectContext(prepared, diff, input.Anchor)
}

// selectContext resolves references only within a verified comparison and copies literal lines.
func selectContext(prepared git.Prepared, diff git.Diff, anchor Anchor) (Context, error) {
	if err := anchor.Validate(); err != nil {
		return Context{}, ErrInvalidSelection
	}
	id, err := prepared.Revision.ID()
	if err != nil || !diff.Complete || diff.Revision != prepared.Revision || diff.RevisionID != id || prepared.Directory == "" {
		return Context{}, ErrInvalidSelection
	}
	for _, file := range diff.Files {
		if file.ID != anchor.FileID {
			continue
		}
		if file.Kind != "text" {
			return Context{}, ErrInvalidSelection
		}
		for _, hunk := range file.Hunks {
			if hunk.ID != anchor.HunkID {
				continue
			}
			selected := make([]git.DiffLine, 0)
			changed := false
			for _, line := range hunk.Lines {
				coordinate := line.NewLine
				if anchor.Side == "old" {
					coordinate = line.OldLine
				}
				if anchor.Kind == "hunk" || coordinate >= anchor.Start && coordinate <= anchor.End {
					selected = append(selected, line)
					changed = changed || line.Sign == "+" || line.Sign == "-"
				}
			}
			if !changed || anchor.Kind == "lines" && len(selected) != anchor.End-anchor.Start+1 {
				return Context{}, ErrInvalidSelection
			}
			if len(selected) > 1000 {
				return Context{}, ErrContextLimit
			}
			size := len(file.OldPath) + len(file.NewPath) + len(hunk.Section)
			for _, lines := range [][]git.DiffLine{selected, hunk.Lines} {
				for _, line := range lines {
					size += len(line.Text) + 1
					if size > MaxContextBytes {
						return Context{}, ErrContextLimit
					}
				}
			}
			hunk.Lines = append([]git.DiffLine(nil), hunk.Lines...)
			return Context{Revision: prepared.Revision, RevisionID: id, Anchor: anchor, OldPath: file.OldPath, NewPath: file.NewPath, Selected: selected, Surrounding: hunk, Directory: prepared.Directory}, nil
		}
	}
	return Context{}, ErrInvalidSelection
}

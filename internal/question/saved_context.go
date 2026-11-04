package question

import (
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// validateSavedContext checks quote consistency without treating saved text as verified source.
func validateSavedContext(c Context) error {
	invalid := errors.New("invalid saved question context")
	id, err := c.Revision.ID()
	h := c.Surrounding
	if err != nil || id != c.RevisionID || c.Anchor.Validate() != nil || c.Directory != "" || h.ID != c.Anchor.HunkID || !githubpr.ValidID(h.Old.ID) || !githubpr.ValidID(h.New.ID) || h.Old.Start < 0 || h.New.Start < 0 || h.Old.Count < 0 || h.New.Count < 0 || h.Old.Start > 1_000_100_000 || h.New.Start > 1_000_100_000 || h.Old.Count > 100_000 || h.New.Count > 100_000 || len(h.Lines) > 100_000 || len(c.Selected) == 0 || len(c.Selected) > 1000 || c.Selected == nil || h.Lines == nil || c.OldPath == "" && c.NewPath == "" {
		return invalid
	}
	size := len(c.OldPath) + len(c.NewPath) + len(h.Section)
	for _, text := range []string{c.OldPath, c.NewPath, h.Section} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return invalid
		}
	}
	selected := []git.DiffLine{}
	old, newLine := h.Old.Start, h.New.Start
	oldCount, newCount := 0, 0
	ids := map[string]bool{}
	changed := false
	for _, line := range h.Lines {
		if !githubpr.ValidID(line.ID) || ids[line.ID] || !utf8.ValidString(line.Text) || strings.ContainsAny(line.Text, "\x00\n") {
			return invalid
		}
		ids[line.ID] = true
		switch line.Sign {
		case " ":
			if line.OldLine != old || line.NewLine != newLine || old <= 0 || newLine <= 0 {
				return invalid
			}
			old++
			newLine++
			oldCount++
			newCount++
		case "-":
			if line.OldLine != old || line.NewLine != 0 || old <= 0 {
				return invalid
			}
			old++
			oldCount++
		case "+":
			if line.NewLine != newLine || line.OldLine != 0 || newLine <= 0 {
				return invalid
			}
			newLine++
			newCount++
		default:
			return invalid
		}
		coordinate := line.NewLine
		if c.Anchor.Side == "old" {
			coordinate = line.OldLine
		}
		if c.Anchor.Kind == "hunk" || coordinate >= c.Anchor.Start && coordinate <= c.Anchor.End {
			selected = append(selected, line)
			changed = changed || line.Sign == "+" || line.Sign == "-"
		}
		size += len(line.Text) + 1
	}
	for _, line := range c.Selected {
		size += len(line.Text) + 1
	}
	if oldCount != h.Old.Count || newCount != h.New.Count || old > 1_000_100_001 || newLine > 1_000_100_001 || !changed || !reflect.DeepEqual(selected, c.Selected) || c.Anchor.Kind == "lines" && len(selected) != c.Anchor.End-c.Anchor.Start+1 || size > MaxContextBytes {
		return invalid
	}
	return nil
}

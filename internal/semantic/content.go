package semantic

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// Anchor identifies one original contiguous side of a changed text hunk.
type Anchor struct {
	FileID string `json:"fileID"`
	HunkID string `json:"hunkID"`
	Side   string `json:"side"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

// Group references literal inventory rather than model-generated source.
type Group struct {
	ID          string   `json:"groupID"`
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	Ranges      []Anchor `json:"ranges"`
}

// Content stores the complete source once, keeping coverage derivable after cache loss.
type Content struct {
	Diff   git.Diff `json:"diff"`
	Groups []Group  `json:"groups"`
}

type nativeGroup struct {
	Title       string        `json:"title"`
	Explanation string        `json:"explanation"`
	Ranges      []nativeRange `json:"ranges"`
}

type nativeRange struct {
	Path  string `json:"path"`
	Side  string `json:"side"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Parse validates complete native output and all original references before publication.
func (s *Source) Parse(data []byte) (Content, error) {
	if s == nil || s.directory == "" || s.prompt == "" {
		return Content{}, ErrSource
	}
	if len(data) > MaxOutputBytes {
		return Content{}, ErrLimit
	}
	var output struct {
		Groups []nativeGroup `json:"groups"`
	}
	if !decode(data, &output) {
		return Content{}, ErrOutput
	}
	result := Content{Diff: s.diff, Groups: []Group{}}
	for i, g := range output.Groups {
		ranges := []Anchor{}
		for _, reference := range g.Ranges {
			anchor, err := s.resolveRange(reference)
			if err != nil {
				return Content{}, err
			}
			ranges = append(ranges, anchor)
		}
		result.Groups = append(result.Groups, Group{ID: "g" + strconv.Itoa(i+1), Title: g.Title, Explanation: g.Explanation, Ranges: ranges})
	}
	if err := result.Validate(); err != nil {
		return Content{}, err
	}
	encoded, _ := json.Marshal(result)
	if len(encoded)+1 > MaxContentBytes {
		return Content{}, ErrLimit
	}
	var detached Content
	if json.Unmarshal(encoded, &detached) != nil {
		return Content{}, ErrOutput
	}
	return detached, nil
}

// resolveRange maps exact side-specific paths and coordinates to one authoritative original hunk.
func (s *Source) resolveRange(reference nativeRange) (Anchor, error) {
	if reference.Path == "" {
		return Anchor{}, ErrOutput
	}
	var resolved Anchor
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
			a := Anchor{FileID: file.ID, HunkID: hunk.ID, Side: reference.Side, Start: reference.Start, End: reference.End}
			if _, err := (Content{Diff: s.diff}).Lines(a); err == nil {
				if found {
					return Anchor{}, ErrOutput
				}
				resolved, found = a, true
			}
		}
	}
	if !found {
		return Anchor{}, ErrOutput
	}
	return resolved, nil
}

// Lines resolves a supported range to literal original lines without host-file access.
func (c Content) Lines(a Anchor) ([]git.DiffLine, error) {
	if !githubpr.ValidID(a.FileID) || !githubpr.ValidID(a.HunkID) || a.Side != "old" && a.Side != "new" || a.Start <= 0 || a.End < a.Start || a.End > 1_000_100_000 || a.End-a.Start >= 200 {
		return nil, ErrOutput
	}
	for _, f := range c.Diff.Files {
		if f.ID != a.FileID || f.Kind != "text" {
			continue
		}
		for _, h := range f.Hunks {
			if h.ID != a.HunkID {
				continue
			}
			lines := []git.DiffLine{}
			changed := false
			for _, l := range h.Lines {
				n := l.NewLine
				if a.Side == "old" {
					n = l.OldLine
				}
				if n >= a.Start && n <= a.End {
					lines = append(lines, l)
					changed = changed || a.Side == "old" && l.Sign == "-" || a.Side == "new" && l.Sign == "+"
				}
			}
			if len(lines) != a.End-a.Start+1 || !changed {
				return nil, ErrOutput
			}
			return lines, nil
		}
	}
	return nil, ErrOutput
}

// Validate checks saved inventory, distinct bounded groups and disjoint changed-line assignment.
func (c Content) Validate() error {
	if validateDiff(c.Diff) != nil || len(c.Groups) == 0 || len(c.Groups) > 32 {
		return ErrOutput
	}
	seen := map[string]bool{}
	titles := map[string]bool{}
	total := 0
	for i, g := range c.Groups {
		title := strings.ToLower(strings.TrimSpace(g.Title))
		if g.ID != "g"+strconv.Itoa(i+1) || !validText(g.Title, 160) || titles[title] || !validText(g.Explanation, 4096) || len(g.Ranges) == 0 || len(g.Ranges) > 32 {
			return ErrOutput
		}
		titles[title] = true
		for _, a := range g.Ranges {
			total++
			if total > 128 {
				return ErrLimit
			}
			lines, err := c.Lines(a)
			if err != nil {
				return err
			}
			for _, l := range lines {
				if l.Sign == " " {
					continue
				}
				if seen[l.ID] {
					return ErrOutput
				}
				seen[l.ID] = true
			}
		}
	}
	return nil
}

// Ungrouped derives remaining changed-line ranges; context and unsupported files never inflate coverage.
func (c Content) Ungrouped() []Anchor {
	used := map[string]bool{}
	for _, g := range c.Groups {
		for _, a := range g.Ranges {
			lines, _ := c.Lines(a)
			for _, l := range lines {
				if l.Sign != " " {
					used[l.ID] = true
				}
			}
		}
	}
	result := []Anchor{}
	for _, f := range c.Diff.Files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.Sign == " " || used[l.ID] {
					continue
				}
				a := Anchor{FileID: f.ID, HunkID: h.ID, Side: "new", Start: l.NewLine, End: l.NewLine}
				if l.Sign == "-" {
					a.Side = "old"
					a.Start = l.OldLine
					a.End = l.OldLine
				}
				result = append(result, a)
			}
		}
	}
	return result
}

// validText validates bounded literal display content without rewriting it.
func validText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0)
}

// String withholds generated source and explanations in diagnostics.
func (c Content) String() string { return "semantic review content (private details withheld)" }

// GoString withholds generated source in debug formatting.
func (c Content) GoString() string { return c.String() }

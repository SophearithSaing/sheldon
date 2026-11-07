package semantic

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// fixture supplies synthetic validated source; it does not claim native Git verification.
func fixture(t *testing.T) (*Source, Anchor) {
	t.Helper()
	pr, err := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	r := githubpr.Revision{PRID: pr.ID, BaseBranch: "target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	id, _ := r.ID()
	a := Anchor{FileID: strings.Repeat("1", 64), HunkID: strings.Repeat("2", 64), Side: "new", Start: 2, End: 2}
	h := git.DiffHunk{ID: a.HunkID, Old: git.DiffRange{ID: strings.Repeat("3", 64), Start: 1, Count: 2}, New: git.DiffRange{ID: strings.Repeat("4", 64), Start: 1, Count: 3}, Lines: []git.DiffLine{
		{ID: strings.Repeat("5", 64), Sign: " ", Text: "context", OldLine: 1, NewLine: 1},
		{ID: strings.Repeat("6", 64), Sign: "-", Text: "old", OldLine: 2},
		{ID: strings.Repeat("7", 64), Sign: "+", Text: "<new>界\r", NewLine: 2},
		{ID: strings.Repeat("8", 64), Sign: "+", Text: "another", NewLine: 3, NoNewline: true},
	}}
	d := git.Diff{Revision: r, RevisionID: id, Complete: true, Files: []git.DiffFile{{ID: a.FileID, Status: "modified", Kind: "text", OldPath: "old.txt", NewPath: "new.txt", Hunks: []git.DiffHunk{h}}, {ID: strings.Repeat("9", 64), Status: "modified", Kind: "binary", NewPath: "image.png", Hunks: []git.DiffHunk{}}}}
	s, err := sourceContext(git.Prepared{Revision: r, Directory: "/private/pinned"}, d)
	if err != nil {
		t.Fatal(err)
	}
	return s, a
}

// native serializes explicit synthetic grouping output for validator tests.
func native(t *testing.T, groups ...nativeGroup) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		Groups []nativeGroup `json:"groups"`
	}{groups})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSeparateRangesAndRemainder(t *testing.T) {
	s, a := fixture(t)
	b := a
	b.Start = 3
	b.End = 3
	c, err := s.Parse(native(t, nativeGroup{"First", "Why", []Anchor{a}}, nativeGroup{"Second", "Other purpose", []Anchor{b}}))
	if err != nil {
		t.Fatal(err)
	}
	remaining := c.Ungrouped()
	if len(remaining) != 1 || remaining[0].Side != "old" || remaining[0].Start != 2 {
		t.Fatalf("remaining: %v", remaining)
	}
	lines, err := c.Lines(a)
	if err != nil || lines[0].Text != "<new>界\r" {
		t.Fatal("lost literal source")
	}
	c.Diff.Files[0].Hunks[0].Lines[2].Text = "mutated"
	if s.diff.Files[0].Hunks[0].Lines[2].Text != "<new>界\r" {
		t.Fatal("returned source aliases retained source")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", s, s), "/private") || strings.Contains(s.Prompt(), "/private") {
		t.Fatal("private location exposed")
	}
}

func TestCrossFileGroups(t *testing.T) {
	s, a := fixture(t)
	f := s.diff.Files[0]
	f.ID = strings.Repeat("a", 64)
	f.OldPath = "test.go"
	f.NewPath = "test.go"
	f.Hunks = append([]git.DiffHunk(nil), f.Hunks...)
	f.Hunks[0].ID = strings.Repeat("b", 64)
	f.Hunks[0].Lines = append([]git.DiffLine(nil), f.Hunks[0].Lines...)
	for i := range f.Hunks[0].Lines {
		f.Hunks[0].Lines[i].ID = fmt.Sprintf("%064x", 100+i)
	}
	s.diff.Files = append(s.diff.Files, f)
	b := a
	b.FileID = f.ID
	b.HunkID = f.Hunks[0].ID
	c, err := s.Parse(native(t, nativeGroup{"Guard and test", "Related changes", []Anchor{a, b}}))
	if err != nil || len(c.Groups) != 1 || len(c.Groups[0].Ranges) != 2 {
		t.Fatalf("cross file: %v", err)
	}
}

func TestRejectsInvalidReferencesAndOutput(t *testing.T) {
	for _, name := range []string{"overlap", "context", "foreign", "side", "cross-hunk", "empty", "unknown", "duplicate", "alias", "surrogate", "fenced", "trailing", "oversized", "title"} {
		t.Run(name, func(t *testing.T) {
			s, a := fixture(t)
			groups := []nativeGroup{{"Purpose", "Why", []Anchor{a}}}
			var data []byte
			switch name {
			case "overlap":
				groups[0].Ranges = append(groups[0].Ranges, a)
			case "context":
				groups[0].Ranges[0].Start = 1
				groups[0].Ranges[0].End = 1
			case "foreign":
				groups[0].Ranges[0].FileID = strings.Repeat("f", 64)
			case "side":
				groups[0].Ranges[0].Side = "head"
			case "cross-hunk":
				groups[0].Ranges[0].End = 4
			case "empty":
				groups = nil
			case "title":
				groups[0].Title = strings.Repeat("x", 161)
			}
			data = native(t, groups...)
			switch name {
			case "unknown":
				data = []byte(`{"groups":[],"code":"bad"}`)
			case "duplicate":
				data = []byte(`{"groups":[],"groups":[]}`)
			case "alias":
				data = []byte(`{"Groups":[]}`)
			case "surrogate":
				data = []byte(`{"groups":[{"title":"\ud800"}]}`)
			case "fenced":
				data = append([]byte("```"), data...)
			case "trailing":
				data = append(data, []byte(`{}`)...)
			case "oversized":
				data = make([]byte, MaxOutputBytes+1)
			}
			c, err := s.Parse(data)
			if err == nil || len(c.Groups) != 0 || c.Diff.Complete {
				t.Fatal("invalid output published")
			}
		})
	}
}

func TestSourceRefusesInvalidInventory(t *testing.T) {
	for _, name := range []string{"digest", "complete", "coordinates", "duplicate-line", "text-empty", "budget"} {
		t.Run(name, func(t *testing.T) {
			s, _ := fixture(t)
			d := s.diff
			switch name {
			case "digest":
				d.RevisionID = strings.Repeat("f", 64)
			case "complete":
				d.Complete = false
			case "coordinates":
				d.Files[0].Hunks[0].Lines[2].NewLine = 20
			case "duplicate-line":
				d.Files[0].Hunks[0].Lines[2].ID = d.Files[0].Hunks[0].Lines[0].ID
			case "text-empty":
				d.Files = d.Files[1:]
			case "budget":
				d.Files[1].Reason = strings.Repeat("x", MaxPromptBytes)
			}
			if _, err := sourceContext(git.Prepared{Revision: d.Revision, Directory: "/pinned"}, d); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"groups":[]}`))
	f.Add([]byte(`{"groups":null}`))
	f.Add([]byte(`{"groups":[{"title":"\ud800"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, _ := fixture(t)
		c, err := s.Parse(data)
		if err == nil && c.Validate() != nil {
			t.Fatal("invalid success")
		}
	})
}

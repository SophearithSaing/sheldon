package question

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
)

// selectionFixture models two verified hunks with both source sides and literal line content.
func selectionFixture(t *testing.T) (git.Prepared, git.Diff, Anchor) {
	t.Helper()
	_, input, _, _ := fixture(t)
	prepared := git.Prepared{Revision: input.Revision, Directory: "/private/worktree"}
	id, _ := input.Revision.ID()
	anchor := input.Anchor
	anchor.Start, anchor.End = 2, 3
	hunk := git.DiffHunk{ID: anchor.HunkID, Old: git.DiffRange{Start: 1, Count: 4}, New: git.DiffRange{Start: 1, Count: 4}, Lines: []git.DiffLine{
		{ID: strings.Repeat("1", 64), Sign: " ", Text: "context", OldLine: 1, NewLine: 1},
		{ID: strings.Repeat("2", 64), Sign: "-", Text: "\told <source>\r", OldLine: 2},
		{ID: strings.Repeat("3", 64), Sign: "-", Text: "old end", OldLine: 3, NoNewline: true},
		{ID: strings.Repeat("4", 64), Sign: "+", Text: "\tnew <source>\r", NewLine: 2},
		{ID: strings.Repeat("5", 64), Sign: "+", Text: "new end", NewLine: 3, NoNewline: true},
		{ID: strings.Repeat("6", 64), Sign: " ", Text: "context end", OldLine: 4, NewLine: 4},
	}}
	diff := git.Diff{Revision: prepared.Revision, RevisionID: id, Complete: true, Files: []git.DiffFile{{ID: anchor.FileID, Kind: "text", OldPath: "old name.txt", NewPath: "new name.txt", Hunks: []git.DiffHunk{hunk, {ID: strings.Repeat("7", 64), Lines: []git.DiffLine{{Sign: "+", Text: "other hunk", NewLine: 20}}}}}}}
	return prepared, diff, anchor
}

func TestSelectionDerivesLiteralOldNewAndHunkContext(t *testing.T) {
	for _, kind := range []string{"old", "new", "hunk"} {
		t.Run(kind, func(t *testing.T) {
			prepared, diff, anchor := selectionFixture(t)
			anchor.Side = kind
			if kind == "hunk" {
				anchor.Kind, anchor.Side, anchor.Start, anchor.End = "hunk", "", 0, 0
			}
			got, err := selectContext(prepared, diff, anchor)
			if err != nil || got.Revision != prepared.Revision || got.Directory != prepared.Directory || got.Anchor != anchor || got.OldPath != "old name.txt" || got.NewPath != "new name.txt" {
				t.Fatalf("context: %+v %v", got, err)
			}
			if !reflect.DeepEqual(got.Surrounding, diff.Files[0].Hunks[0]) {
				t.Fatal("surrounding source changed")
			}
			want := diff.Files[0].Hunks[0].Lines
			if kind == "old" {
				want = want[1:3]
			} else if kind == "new" {
				want = want[3:5]
			}
			if !reflect.DeepEqual(got.Selected, want) {
				t.Fatal("selection text, coordinates or final-LF marker changed")
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), prepared.Directory) || strings.Contains(string(encoded), "Directory") {
				t.Fatal("backend directory leaked")
			}
			got.Selected[0].Text = "local edit"
			got.Surrounding.Lines[0].Text = "local edit"
			if diff.Files[0].Hunks[0].Lines[0].Text != "context" || reflect.DeepEqual(got.Selected, want) {
				t.Fatal("context aliases its source model")
			}
		})
	}
}

func TestSelectionRejectsUnboundAndUnsupportedSource(t *testing.T) {
	for _, kind := range []string{"file", "hunk", "cross hunk", "absent coordinates", "context only", "wrong side", "incomplete", "wrong revision", "wrong digest", "no worktree", "binary", "submodule", "mode-only", "symlink", "type-change", "metadata-only", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			prepared, diff, anchor := selectionFixture(t)
			switch kind {
			case "file":
				anchor.FileID = strings.Repeat("8", 64)
			case "hunk":
				anchor.HunkID = strings.Repeat("8", 64)
			case "cross hunk":
				anchor.End = 20
			case "absent coordinates":
				anchor.Start, anchor.End = 10, 11
			case "context only":
				anchor.Start, anchor.End = 1, 1
			case "wrong side":
				diff.Files[0].Hunks[0].Lines = diff.Files[0].Hunks[0].Lines[3:5]
			case "incomplete":
				diff.Complete = false
			case "wrong revision":
				diff.Revision.BaseSHA = strings.Repeat("9", 40)
			case "wrong digest":
				diff.RevisionID = strings.Repeat("9", 64)
			case "no worktree":
				prepared.Directory = ""
			default:
				diff.Files[0].Kind = kind
			}
			got, err := selectContext(prepared, diff, anchor)
			if !errors.Is(err, ErrInvalidSelection) || got.Directory != "" || len(got.Selected) != 0 {
				t.Fatalf("invalid selection returned context: %+v %v", got, err)
			}
		})
	}
}

func TestSelectionBoundsIncludeSurroundingAndRepeatedQuotes(t *testing.T) {
	for _, kind := range []string{"hunk lines", "selected bytes", "surrounding bytes"} {
		t.Run(kind, func(t *testing.T) {
			prepared, diff, anchor := selectionFixture(t)
			hunk := &diff.Files[0].Hunks[0]
			switch kind {
			case "hunk lines":
				anchor.Kind, anchor.Side, anchor.Start, anchor.End = "hunk", "", 0, 0
				hunk.Lines = make([]git.DiffLine, 1001)
				for i := range hunk.Lines {
					hunk.Lines[i] = git.DiffLine{Sign: "+", NewLine: i + 1, Text: "x"}
				}
			case "selected bytes":
				hunk.Lines[1].Text = strings.Repeat("界", MaxContextBytes/3)
			case "surrounding bytes":
				hunk.Lines[0].Text = strings.Repeat("x", MaxContextBytes)
			}
			got, err := selectContext(prepared, diff, anchor)
			if !errors.Is(err, ErrContextLimit) || len(got.Selected) != 0 {
				t.Fatalf("oversized context was truncated/accepted: %v", err)
			}
		})
	}
}

func TestResolveValidatesBeforeCacheAccess(t *testing.T) {
	registry, input, _, _ := fixture(t)
	if _, err := Resolve(context.Background(), registry.pr.ID, registry, nil, input); !errors.Is(err, git.ErrQuestionSourceUnavailable) {
		t.Fatal("missing cache did not fail explicitly")
	}
	input.Question = " "
	if _, err := Resolve(context.Background(), registry.pr.ID, registry, nil, input); err == nil || errors.Is(err, git.ErrQuestionSourceUnavailable) {
		t.Fatal("invalid input reached source resolution")
	}
}

func TestSelectionAcceptsExactContextAndLineLimits(t *testing.T) {
	prepared, diff, anchor := selectionFixture(t)
	anchor.Side, anchor.Start, anchor.End = "new", 1, 1
	file := &diff.Files[0]
	file.OldPath, file.NewPath = "a", "a"
	file.Hunks[0].Section = ""
	file.Hunks[0].Lines = []git.DiffLine{{Sign: "+", NewLine: 1, Text: strings.Repeat("x", (MaxContextBytes-4)/2)}}
	if _, err := selectContext(prepared, diff, anchor); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	file.Hunks[0].Lines[0].Text += "x"
	if _, err := selectContext(prepared, diff, anchor); !errors.Is(err, ErrContextLimit) {
		t.Fatal("byte overflow accepted")
	}
	anchor.Kind, anchor.Side, anchor.Start, anchor.End = "hunk", "", 0, 0
	file.Hunks[0].Lines = make([]git.DiffLine, 1000)
	for i := range file.Hunks[0].Lines {
		file.Hunks[0].Lines[i] = git.DiffLine{Sign: "+", NewLine: i + 1, Text: "x"}
	}
	if got, err := selectContext(prepared, diff, anchor); err != nil || len(got.Selected) != 1000 {
		t.Fatalf("exact line limit rejected: %v", err)
	}
}

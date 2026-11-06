package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// sourceFixture models an already-verified changed hunk and a non-text inventory entry.
func sourceFixture(t *testing.T) (git.Prepared, git.Diff, Anchor) {
	t.Helper()
	pr, err := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	id, _ := revision.ID()
	anchor := Anchor{FileID: strings.Repeat("1", 64), HunkID: strings.Repeat("2", 64), Side: "new", Start: 2, End: 2}
	hunk := git.DiffHunk{ID: anchor.HunkID, Old: git.DiffRange{ID: strings.Repeat("3", 64), Start: 1, Count: 3}, New: git.DiffRange{ID: strings.Repeat("4", 64), Start: 1, Count: 3}, Section: " function", Lines: []git.DiffLine{
		{ID: strings.Repeat("5", 64), Sign: " ", Text: "context", OldLine: 1, NewLine: 1},
		{ID: strings.Repeat("6", 64), Sign: "-", Text: "\told <code>\r", OldLine: 2},
		{ID: strings.Repeat("7", 64), Sign: "+", Text: "\tnew <code>界\r", NewLine: 2, NoNewline: true},
		{ID: strings.Repeat("8", 64), Sign: " ", Text: "context end", OldLine: 3, NewLine: 3},
	}}
	diff := git.Diff{Revision: revision, RevisionID: id, Complete: true, Files: []git.DiffFile{
		{ID: anchor.FileID, Status: "renamed", Kind: "text", OldPath: "old name.txt", NewPath: "new 界.txt", Hunks: []git.DiffHunk{hunk}},
		{ID: strings.Repeat("9", 64), Status: "modified", Kind: "binary", OldPath: "image.png", NewPath: "image.png", Reason: "binary", Hunks: []git.DiffHunk{}},
	}}
	return git.Prepared{Revision: revision, Directory: "/private/pinned-worktree"}, diff, anchor
}

// preparedSource returns private context for pure validation without executing Git or inference.
func preparedSource(t *testing.T) (*Source, Anchor) {
	t.Helper()
	prepared, diff, anchor := sourceFixture(t)
	source, err := sourceContext(prepared, diff)
	if err != nil {
		t.Fatal(err)
	}
	return source, anchor
}

func TestSourcePromptRetainsCompleteManifestAndWithholdsDirectory(t *testing.T) {
	prepared, diff, _ := sourceFixture(t)
	source, err := sourceContext(prepared, diff)
	if err != nil || source.Directory() != prepared.Directory {
		t.Fatalf("source: %v", err)
	}
	var prompt struct {
		Instructions string   `json:"instructions"`
		Source       git.Diff `json:"source"`
	}
	if json.Unmarshal([]byte(source.Prompt()), &prompt) != nil || len(prompt.Source.Files) != 2 || prompt.Source.Revision != diff.Revision || prompt.Source.Files[1].Kind != "binary" || prompt.Source.Files[0].Hunks[0].Lines[2].Text != diff.Files[0].Hunks[0].Lines[2].Text {
		t.Fatal("prompt omitted or changed source inventory")
	}
	for _, instruction := range []string{"five", "a,b,c,d", "merge base", "untrusted data", "131072", "8192", "200 lines", "source-grounded"} {
		if !strings.Contains(prompt.Instructions, instruction) {
			t.Fatalf("missing instruction %q", instruction)
		}
	}
	encoded, _ := json.Marshal(source)
	if string(encoded) != "{}" || strings.Contains(source.Prompt(), prepared.Directory) || strings.Contains(fmt.Sprintf("%v %#v", source, source), prepared.Directory) || strings.Contains(fmt.Sprintf("%v", source), "<code>") {
		t.Fatal("private source exposed by serialization/formatting")
	}
}

func TestSourceRejectsIncompleteMismatchedAndTextEmptyDiffs(t *testing.T) {
	for _, name := range []string{"incomplete", "revision", "digest", "directory", "empty", "non-text", "unchanged"} {
		t.Run(name, func(t *testing.T) {
			prepared, diff, _ := sourceFixture(t)
			switch name {
			case "incomplete":
				diff.Complete = false
			case "revision":
				diff.Revision.BaseSHA = strings.Repeat("d", 40)
			case "digest":
				diff.RevisionID = strings.Repeat("d", 64)
			case "directory":
				prepared.Directory = ""
			case "empty":
				diff.Files = nil
			case "non-text":
				diff.Files[0].Kind = "binary"
			case "unchanged":
				diff.Files[0].Hunks[0].Lines = nil
			}
			if source, err := sourceContext(prepared, diff); !errors.Is(err, ErrSource) || source != nil {
				t.Fatalf("invalid context accepted: %v", err)
			}
		})
	}
}

func TestSourcePromptExactEncodedBudgetAndOverflow(t *testing.T) {
	prepared, diff, _ := sourceFixture(t)
	source, err := sourceContext(prepared, diff)
	if err != nil {
		t.Fatal(err)
	}
	line := &diff.Files[0].Hunks[0].Lines[0]
	line.Text += strings.Repeat("x", MaxPromptBytes-len(source.Prompt()))
	if source, err = sourceContext(prepared, diff); err != nil || len(source.Prompt()) != MaxPromptBytes {
		t.Fatalf("exact serialized prompt budget refused: %v", err)
	}
	line.Text += "x"
	if source, err = sourceContext(prepared, diff); !errors.Is(err, ErrLimit) || source != nil {
		t.Fatalf("oversized prompt sampled/truncated: %v", err)
	}
	line.Text = strings.Repeat("<", MaxPromptBytes/6)
	if source, err = sourceContext(prepared, diff); !errors.Is(err, ErrLimit) || source != nil {
		t.Fatal("JSON escaping not included in prompt budget")
	}
}

type fixtureRegistry struct {
	pr  githubpr.PullRequest
	err error
}

// ReadPR returns only this fixture's authoritative registration.
func (r fixtureRegistry) ReadPR(id string) (githubpr.PullRequest, error) {
	return r.pr, r.err
}

func TestResolveRejectsRegistrationAndRevisionBeforeSourceAccess(t *testing.T) {
	t.Setenv("PATH", "")
	prepared, _, _ := sourceFixture(t)
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	for _, name := range []string{"registry missing", "registry error", "foreign PR", "invalid PR", "foreign revision", "invalid ID", "cache missing"} {
		t.Run(name, func(t *testing.T) {
			var registry Registry = fixtureRegistry{pr: pr}
			revision, id := prepared.Revision, pr.ID
			switch name {
			case "registry missing":
				registry = nil
			case "registry error":
				registry = fixtureRegistry{pr: pr, err: errors.New("private storage diagnostics")}
			case "foreign PR":
				other, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/2")
				registry = fixtureRegistry{pr: other}
			case "invalid PR":
				registry = fixtureRegistry{}
			case "foreign revision":
				revision.PRID = strings.Repeat("f", 64)
			case "invalid ID":
				id = "../private"
			}
			source, err := Resolve(context.Background(), id, registry, nil, revision)
			if source != nil || !errors.Is(err, ErrSource) || strings.Contains(err.Error(), "diagnostics") {
				t.Fatalf("invalid scope leaked or accepted: %v", err)
			}
		})
	}
}

package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// ErrIncompleteDiff indicates that no complete, usable comparison was produced.
var ErrIncompleteDiff = errors.New("the pinned diff could not be produced completely")

// Diff contains a complete comparison of immutable merge-base and head objects.
type Diff struct {
	Revision   githubpr.Revision `json:"revision"`
	RevisionID string            `json:"revisionID"`
	Complete   bool              `json:"complete"`
	Files      []DiffFile        `json:"files"`
}

// DiffFile identifies a changed path pair; paths are display data, not host access grants.
type DiffFile struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	OldPath    string     `json:"oldPath,omitempty"`
	NewPath    string     `json:"newPath,omitempty"`
	OldMode    string     `json:"oldMode"`
	NewMode    string     `json:"newMode"`
	OldObject  string     `json:"oldObject"`
	NewObject  string     `json:"newObject"`
	Similarity int        `json:"similarity,omitempty"`
	Kind       string     `json:"kind"`
	Reason     string     `json:"reason,omitempty"`
	Hunks      []DiffHunk `json:"hunks"`
}

// DiffRange is a Git hunk range, including zero-count insertion/deletion boundaries.
type DiffRange struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	Count int    `json:"count"`
}

// DiffHunk groups literal lines and revision-scoped references to both source ranges.
type DiffHunk struct {
	ID      string     `json:"id"`
	Old     DiffRange  `json:"old"`
	New     DiffRange  `json:"new"`
	Section string     `json:"section"`
	Lines   []DiffLine `json:"lines"`
}

// DiffLine preserves a source line without its diff sign or terminating LF.
type DiffLine struct {
	ID        string `json:"id"`
	Sign      string `json:"sign"`
	Text      string `json:"text"`
	OldLine   int    `json:"oldLine,omitempty"`
	NewLine   int    `json:"newLine,omitempty"`
	NoNewline bool   `json:"noNewline,omitempty"`
}

// Diff produces a bounded unified comparison without reading or modifying a worktree.
func (c *Cache) Diff(ctx context.Context, pr githubpr.PullRequest, revision githubpr.Revision) (Diff, error) {
	if err := pr.Validate(); err != nil {
		return Diff{}, err
	}
	if err := revision.Validate(pr.ID); err != nil {
		return Diff{}, err
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	repo, release, err := c.existingRepository(ctx, pr)
	if err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	defer release()
	for _, sha := range []string{revision.BaseSHA, revision.HeadSHA, revision.MergeBaseSHA} {
		if err := c.commit(ctx, repo, sha); err != nil {
			return Diff{}, errors.Join(ErrIncompleteDiff, err)
		}
	}
	merge, err := c.mergeBase(ctx, repo, revision.BaseSHA, revision.HeadSHA)
	if err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	if merge != revision.MergeBaseSHA {
		return Diff{}, errors.Join(ErrIncompleteDiff, errors.New("pinned merge-base identity does not match the comparison"))
	}
	if err := c.retain(ctx, repo, revision); err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	output, err := c.run(ctx, repo, "-c", "core.quotePath=true", "-c", "diff.suppressBlankEmpty=false",
		"diff", "--raw", "-z", "--patch", "--no-abbrev", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"--no-relative", "--src-prefix=a/", "--dst-prefix=b/", "--line-prefix=", "--output-indicator-old=-", "--output-indicator-new=+", "--output-indicator-context= ",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--unified=3", "--inter-hunk-context=0", "--word-diff=none", "--color-moved=no",
		"--find-renames=50%", "--rename-empty", "-l1000", "--ignore-submodules=none", "--submodule=short", "-O"+os.DevNull,
		revision.MergeBaseSHA, revision.HeadSHA, "--")
	if err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	result, err := parseDiff(output, revision)
	if err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	if err := ctx.Err(); err != nil {
		return Diff{}, errors.Join(ErrIncompleteDiff, err)
	}
	return result, nil
}

// diffID binds display references to a revision and an unambiguous sequence of parts.
func diffID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(strconv.Itoa(len(part)) + ":" + part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

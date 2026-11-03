package git

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// ErrSnapshotUnavailable reports missing or changed historical source without fetching replacements.
var ErrSnapshotUnavailable = errors.New("the exact pinned source is unavailable in the managed cache")

// ReadPrepared verifies retained commits and the original worktree without creating or repairing it.
func (c *Cache) ReadPrepared(ctx context.Context, pr githubpr.PullRequest, revision githubpr.Revision) (prepared Prepared, resultErr error) {
	if err := pr.Validate(); err != nil {
		return Prepared{}, err
	}
	if err := revision.Validate(pr.ID); err != nil {
		return Prepared{}, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(ErrSnapshotUnavailable, resultErr)
		}
	}()
	ctx, cancel := operation(ctx)
	defer cancel()
	repo, release, err := c.existingRepository(ctx, pr)
	if err != nil {
		return Prepared{}, err
	}
	defer release()
	id, _ := revision.ID()
	refs, err := c.refs(ctx, repo)
	if err != nil {
		return Prepared{}, err
	}
	for name, sha := range map[string]string{"base": revision.BaseSHA, "head": revision.HeadSHA, "merge-base": revision.MergeBaseSHA} {
		if refs["refs/sheldon/reviews/"+id+"/"+name] != sha {
			return Prepared{}, ErrSnapshotUnavailable
		}
		if err := c.commit(ctx, repo, sha); err != nil {
			return Prepared{}, err
		}
	}
	merge, err := c.mergeBase(ctx, repo, revision.BaseSHA, revision.HeadSHA)
	if err != nil {
		return Prepared{}, err
	}
	if merge != revision.MergeBaseSHA {
		return Prepared{}, ErrSnapshotUnavailable
	}
	for _, dir := range []string{filepath.Join(c.paths.Cache, "worktrees"), filepath.Join(c.paths.Cache, "worktrees", pr.ID)} {
		if err := checkEntry(dir, true); err != nil {
			return Prepared{}, err
		}
	}
	root := filepath.Join(c.paths.Cache, "worktrees", pr.ID, revision.HeadSHA)
	repositoryID, _ := pr.RepositoryID()
	want := worktreeMarker{Version: 1, PRID: pr.ID, RepositoryID: repositoryID, HeadSHA: revision.HeadSHA}
	var marker worktreeMarker
	if err := readJSON(root+".json", &marker); err != nil {
		return Prepared{}, err
	}
	if marker != want {
		return Prepared{}, ErrSnapshotUnavailable
	}
	entries, err := c.sourceEntries(ctx, repo, revision.HeadSHA)
	if err != nil {
		return Prepared{}, err
	}
	if err := c.validateWorktree(ctx, repo, root, revision.HeadSHA, entries); err != nil {
		return Prepared{}, err
	}
	return Prepared{Revision: revision, Directory: root}, nil
}

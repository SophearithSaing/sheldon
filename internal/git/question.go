package git

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

// ErrQuestionSourceUnavailable refuses missing or changed original question source.
var ErrQuestionSourceUnavailable = errors.New("original pinned question source is unavailable; existing source was not changed")

// QuestionSource verifies existing pinned source and reads its diff without fetching or repair.
// The caller must resolve pr through the registered index; Directory stays backend-only.
func (c *Cache) QuestionSource(ctx context.Context, pr githubpr.PullRequest, revision githubpr.Revision) (prepared Prepared, diff Diff, resultErr error) {
	defer func() {
		if resultErr != nil {
			prepared, diff = Prepared{}, Diff{}
			for _, cause := range []error{context.Canceled, context.DeadlineExceeded, process.ErrOutputLimit} {
				if errors.Is(resultErr, cause) {
					resultErr = errors.Join(ErrQuestionSourceUnavailable, cause)
					return
				}
			}
			resultErr = ErrQuestionSourceUnavailable
		}
	}()
	if err := pr.Validate(); err != nil {
		return Prepared{}, Diff{}, err
	}
	if err := revision.Validate(pr.ID); err != nil {
		return Prepared{}, Diff{}, err
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	repo, release, err := c.existingRepository(ctx, pr)
	if err != nil {
		return Prepared{}, Diff{}, err
	}
	defer release()
	id, _ := revision.ID()
	refs, err := c.refs(ctx, repo)
	if err != nil {
		return Prepared{}, Diff{}, err
	}
	for name, sha := range map[string]string{"base": revision.BaseSHA, "head": revision.HeadSHA, "merge-base": revision.MergeBaseSHA} {
		if refs["refs/sheldon/reviews/"+id+"/"+name] != sha {
			return Prepared{}, Diff{}, ErrQuestionSourceUnavailable
		}
		if err := c.commit(ctx, repo, sha); err != nil {
			return Prepared{}, Diff{}, err
		}
	}
	merge, err := c.mergeBase(ctx, repo, revision.BaseSHA, revision.HeadSHA)
	if err != nil || merge != revision.MergeBaseSHA {
		return Prepared{}, Diff{}, errors.Join(ErrQuestionSourceUnavailable, err)
	}
	for _, directory := range []string{filepath.Join(c.paths.Cache, "worktrees"), filepath.Join(c.paths.Cache, "worktrees", pr.ID)} {
		if err := checkEntry(directory, true); err != nil {
			return Prepared{}, Diff{}, err
		}
	}
	root := filepath.Join(c.paths.Cache, "worktrees", pr.ID, revision.HeadSHA)
	repositoryID, _ := pr.RepositoryID()
	var marker worktreeMarker
	if readJSON(root+".json", &marker) != nil || marker != (worktreeMarker{Version: 1, PRID: pr.ID, RepositoryID: repositoryID, HeadSHA: revision.HeadSHA}) {
		return Prepared{}, Diff{}, ErrQuestionSourceUnavailable
	}
	entries, err := c.sourceEntries(ctx, repo, revision.HeadSHA)
	if err != nil {
		return Prepared{}, Diff{}, err
	}
	if err := c.validateWorktree(ctx, repo, root, revision.HeadSHA, entries); err != nil {
		return Prepared{}, Diff{}, err
	}
	diff, err = c.readDiff(ctx, repo, revision)
	if err != nil {
		return Prepared{}, Diff{}, err
	}
	return Prepared{Revision: revision, Directory: root}, diff, nil
}

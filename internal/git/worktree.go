package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// ErrHistoryUnavailable requires complete local history rather than a guessed base.
var ErrHistoryUnavailable = errors.New("complete pinned history is unavailable; fetch full history before preparing the review")

// ErrUnrelatedHistory indicates that the pinned commits have no common ancestor.
var ErrUnrelatedHistory = errors.New("the pinned base and head have unrelated histories")

// ErrAmbiguousMergeBase refuses to choose arbitrarily between multiple merge bases.
var ErrAmbiguousMergeBase = errors.New("the pinned commits have multiple merge bases; this comparison is unsupported")

// Prepared contains validated comparison identities and a backend-only source location.
type Prepared struct {
	Revision  githubpr.Revision
	Directory string `json:"-"`
}

type worktreeMarker struct {
	Version      int    `json:"version"`
	PRID         string `json:"prID"`
	RepositoryID string `json:"repositoryID"`
	HeadSHA      string `json:"headSHA"`
}

// Prepare resolves pinned history and creates or validates a detached head worktree.
func (c *Cache) Prepare(ctx context.Context, pr githubpr.PullRequest, snapshot Snapshot) (result Prepared, resultErr error) {
	if err := pr.Validate(); err != nil {
		return Prepared{}, err
	}
	id, _ := pr.RepositoryID()
	if snapshot.PRID != pr.ID || snapshot.RepositoryID != id || !githubpr.ValidBranch(snapshot.BaseBranch) || !githubpr.ValidCommit(snapshot.BaseSHA) || !githubpr.ValidCommit(snapshot.HeadSHA) || len(snapshot.BaseSHA) != len(snapshot.HeadSHA) || snapshot.BaseSource != githubpr.GitHubBase && snapshot.BaseSource != githubpr.UserSelectedBase {
		return Prepared{}, errors.New("invalid pinned PR snapshot")
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	repo, release, err := c.repository(ctx, pr)
	if err != nil {
		return Prepared{}, err
	}
	defer release()
	for _, sha := range []string{snapshot.BaseSHA, snapshot.HeadSHA} {
		if err := c.commit(ctx, repo, sha); err != nil {
			return Prepared{}, errors.Join(ErrHistoryUnavailable, err)
		}
	}
	merge, err := c.mergeBase(ctx, repo, snapshot.BaseSHA, snapshot.HeadSHA)
	if err != nil {
		return Prepared{}, err
	}
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: snapshot.BaseBranch, BaseSource: snapshot.BaseSource, BaseSHA: snapshot.BaseSHA, HeadSHA: snapshot.HeadSHA, MergeBaseSHA: merge}
	if err := c.retain(ctx, repo, revision); err != nil {
		return Prepared{}, err
	}
	entries, err := c.sourceEntries(ctx, repo, snapshot.HeadSHA)
	if err != nil {
		return Prepared{}, err
	}
	for _, dir := range []string{filepath.Join(c.paths.Cache, "worktrees"), filepath.Join(c.paths.Cache, "worktrees", pr.ID)} {
		if err := privateDirectory(dir); err != nil {
			return Prepared{}, err
		}
	}
	root := filepath.Join(c.paths.Cache, "worktrees", pr.ID, snapshot.HeadSHA)
	markerPath := root + ".json"
	want := worktreeMarker{Version: 1, PRID: pr.ID, RepositoryID: id, HeadSHA: snapshot.HeadSHA}
	_, rootErr := os.Lstat(root)
	_, markerErr := os.Lstat(markerPath)
	if rootErr == nil || markerErr == nil {
		var marker worktreeMarker
		if rootErr != nil || markerErr != nil || readJSON(markerPath, &marker) != nil || marker != want {
			return Prepared{}, errors.New("unidentified, incomplete or missing review worktree; preserve existing cache state")
		}
		if err := c.validateWorktree(ctx, repo, root, snapshot.HeadSHA, entries); err != nil {
			return Prepared{}, err
		}
		return Prepared{Revision: revision, Directory: root}, nil
	}
	if !errors.Is(rootErr, os.ErrNotExist) || !errors.Is(markerErr, os.ErrNotExist) {
		return Prepared{}, errors.New("cannot inspect review worktree paths")
	}
	if err := privateDirectory(filepath.Join(repo, "worktrees")); err != nil {
		return Prepared{}, err
	}
	registrations, err := registrationNames(repo)
	if err != nil {
		return Prepared{}, err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return Prepared{}, err
	}
	owned, err := os.Lstat(root)
	if err != nil {
		return Prepared{}, err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		if current, err := os.Lstat(root); err == nil && os.SameFile(owned, current) {
			if err := c.removeOwnedWorktree(repo, root, registrations); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if _, err := c.run(ctx, repo, "worktree", "add", "--detach", "--no-checkout", "--", root, snapshot.HeadSHA); err != nil {
		return Prepared{}, err
	}
	admin, err := worktreeAdmin(repo, root)
	if err != nil {
		return Prepared{}, err
	}
	if registrations[filepath.Base(admin)] {
		return Prepared{}, errors.New("Git reused an existing worktree registration")
	}
	if err := makeRegistrationPrivate(root, admin, registrations); err != nil {
		return Prepared{}, err
	}
	if err := c.materialize(ctx, repo, root, entries); err != nil {
		return Prepared{}, err
	}
	if _, err := c.run(ctx, root, "read-tree", snapshot.HeadSHA); err != nil {
		return Prepared{}, err
	}
	if err := makeRegistrationPrivate(root, admin, registrations); err != nil {
		return Prepared{}, err
	}
	if err := c.validateRepository(ctx, repo, pr); err != nil {
		return Prepared{}, err
	}
	if err := c.validateWorktree(ctx, repo, root, snapshot.HeadSHA, entries); err != nil {
		return Prepared{}, err
	}
	if err := writeWorktreeMarker(markerPath, want); err != nil {
		return Prepared{}, err
	}
	return Prepared{Revision: revision, Directory: root}, nil
}

// mergeBase requires complete history and one validated common ancestor.
func (c *Cache) mergeBase(ctx context.Context, repo, base, head string) (string, error) {
	shallow, err := c.run(ctx, repo, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return "", err
	}
	if string(shallow) != "false\n" {
		return "", ErrHistoryUnavailable
	}
	if _, err := c.run(ctx, repo, "rev-list", "--parents", base, head, "--"); err != nil {
		return "", errors.Join(ErrHistoryUnavailable, err)
	}
	output, err := c.run(ctx, repo, "merge-base", "--all", base, head)
	if err != nil {
		var command *CommandError
		if errors.As(err, &command) && command.ExitCode == 1 && command.cause == nil {
			return "", ErrUnrelatedHistory
		}
		return "", errors.Join(ErrHistoryUnavailable, err)
	}
	ancestors := strings.Fields(string(output))
	if len(ancestors) > 1 {
		return "", ErrAmbiguousMergeBase
	}
	if len(ancestors) != 1 || !githubpr.ValidCommit(ancestors[0]) || len(ancestors[0]) != len(head) {
		return "", ErrHistoryUnavailable
	}
	if err := c.commit(ctx, repo, ancestors[0]); err != nil {
		return "", errors.Join(ErrHistoryUnavailable, err)
	}
	return ancestors[0], nil
}

// retain pins old comparison objects against later ref movement and ordinary Git GC.
func (c *Cache) retain(ctx context.Context, repo string, revision githubpr.Revision) error {
	id, err := revision.ID()
	if err != nil {
		return err
	}
	refs, err := c.refs(ctx, repo)
	if err != nil {
		return err
	}
	for name, sha := range map[string]string{"base": revision.BaseSHA, "head": revision.HeadSHA, "merge-base": revision.MergeBaseSHA} {
		ref := "refs/sheldon/reviews/" + id + "/" + name
		if existing := refs[ref]; existing != "" {
			if existing != sha {
				return errors.New("retained revision identity changed")
			}
			continue
		}
		if _, err := c.run(ctx, repo, "update-ref", ref, sha, strings.Repeat("0", len(sha))); err != nil {
			return err
		}
	}
	return nil
}

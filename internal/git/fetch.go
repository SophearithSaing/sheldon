package git

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// ErrRefreshRequired reports moved or mismatched commits without returning a partial snapshot.
var ErrRefreshRequired = errors.New("PR or base branch changed; refresh metadata or fetched candidates and select again")

// Snapshot contains fetched and verified commits, not a merge base or ready worktree.
type Snapshot struct {
	PRID         string
	RepositoryID string
	BaseBranch   string
	BaseSHA      string
	HeadSHA      string
	BaseSource   githubpr.BaseSource
}

type ref struct {
	name string
	sha  string
}

// Fetch fetches one PR through the upstream PR ref and verifies its selected snapshot.
func (c *Cache) Fetch(ctx context.Context, resolution github.Resolution) (Snapshot, error) {
	base := resolution.Base
	if resolution.State != github.Resolved || base == nil || !githubpr.ValidBranch(base.Branch) || !githubpr.ValidCommit(base.SHA) || base.Source != githubpr.GitHubBase && base.Source != githubpr.UserSelectedBase {
		return Snapshot{}, errors.New("a resolved actual or explicitly selected base is required")
	}
	if base.Source == githubpr.GitHubBase && (!githubpr.ValidCommit(resolution.HeadSHA) || len(resolution.HeadSHA) != len(base.SHA)) || base.Source == githubpr.UserSelectedBase && resolution.HeadSHA != "" {
		return Snapshot{}, errors.New("invalid reported PR commit identities")
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	dir, release, err := c.repository(ctx, resolution.PR)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	refs, err := c.refs(ctx, dir)
	if err != nil {
		return Snapshot{}, err
	}
	if base.Source == githubpr.UserSelectedBase && refs["refs/sheldon/bases/"+base.Branch] != base.SHA {
		return Snapshot{}, ErrRefreshRequired
	}
	number := strconv.FormatUint(resolution.PR.Number, 10)
	prefix := "refs/sheldon/pr/" + number + "/"
	specs := []string{"+refs/pull/" + number + "/head:" + prefix + "head", "+refs/heads/" + base.Branch + ":" + prefix + "base", "+refs/heads/" + base.Branch + ":refs/sheldon/bases/" + base.Branch}
	if err := c.fetch(ctx, dir, resolution.PR, specs); err != nil {
		return Snapshot{}, err
	}
	if err := c.validateRepository(ctx, dir, resolution.PR); err != nil {
		return Snapshot{}, err
	}
	refs, err = c.refs(ctx, dir)
	if err != nil {
		return Snapshot{}, err
	}
	head := refs[prefix+"head"]
	if refs[prefix+"base"] != base.SHA || refs["refs/sheldon/bases/"+base.Branch] != base.SHA || !githubpr.ValidCommit(head) || len(head) != len(base.SHA) || resolution.HeadSHA != "" && head != resolution.HeadSHA {
		return Snapshot{}, ErrRefreshRequired
	}
	for _, sha := range []string{base.SHA, head} {
		if err := c.commit(ctx, dir, sha); err != nil {
			return Snapshot{}, err
		}
	}
	current, err := c.refs(ctx, dir)
	if err != nil {
		return Snapshot{}, err
	}
	if current[prefix+"head"] != head || current[prefix+"base"] != base.SHA || current["refs/sheldon/bases/"+base.Branch] != base.SHA {
		return Snapshot{}, ErrRefreshRequired
	}
	id, _ := resolution.PR.RepositoryID()
	return Snapshot{PRID: resolution.PR.ID, RepositoryID: id, BaseBranch: base.Branch, BaseSHA: base.SHA, HeadSHA: head, BaseSource: base.Source}, nil
}

// FetchBases fetches advertised upstream branches before offering manual base candidates.
func (c *Cache) FetchBases(ctx context.Context, resolution github.Resolution) ([]github.BaseCandidate, error) {
	if resolution.State != github.BaseSelectionRequired || resolution.Failure == nil || resolution.Base != nil || resolution.HeadSHA != "" {
		return nil, errors.New("choose Git-only fallback before fetching base candidates")
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	dir, release, err := c.repository(ctx, resolution.PR)
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := c.refs(ctx, dir); err != nil {
		return nil, err
	}
	advertised, err := c.run(ctx, dir, "ls-remote", "--heads", "--refs", "--", remoteURL(resolution.PR))
	if err != nil {
		return nil, err
	}
	var branches []ref
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSuffix(string(advertised), "\n"), "\n") {
		if line == "" {
			continue
		}
		sha, name, ok := strings.Cut(line, "\t")
		branch, valid := strings.CutPrefix(name, "refs/heads/")
		if !ok || !valid || !githubpr.ValidBranch(branch) || !githubpr.ValidCommit(sha) || seen[branch] || len(branches) >= 1000 {
			return nil, errors.New("invalid or excessive upstream base branches")
		}
		seen[branch] = true
		branches = append(branches, ref{name: branch, sha: sha})
	}
	if len(branches) == 0 {
		return nil, errors.New("no upstream base branches are available")
	}
	specs := make([]string, 0, len(branches))
	for _, branch := range branches {
		specs = append(specs, "+refs/heads/"+branch.name+":refs/sheldon/bases/"+branch.name)
	}
	if err := c.fetch(ctx, dir, resolution.PR, specs); err != nil {
		return nil, err
	}
	if err := c.validateRepository(ctx, dir, resolution.PR); err != nil {
		return nil, err
	}
	refs, err := c.refs(ctx, dir)
	if err != nil {
		return nil, err
	}
	id, _ := resolution.PR.RepositoryID()
	var candidates []github.BaseCandidate
	for _, branch := range branches {
		if refs["refs/sheldon/bases/"+branch.name] != branch.sha {
			return nil, ErrRefreshRequired
		}
		if err := c.commit(ctx, dir, branch.sha); err != nil {
			return nil, err
		}
		candidates = append(candidates, github.BaseCandidate{RepositoryID: id, Branch: branch.name, SHA: branch.sha})
	}
	return candidates, nil
}

// SelectBase rechecks an explicitly chosen fetched ref under the repository gate.
func (c *Cache) SelectBase(ctx context.Context, resolution github.Resolution, branch, sha string) (github.Resolution, error) {
	if !githubpr.ValidBranch(branch) || !githubpr.ValidCommit(sha) || resolution.State != github.BaseSelectionRequired {
		return github.Resolution{}, errors.New("invalid manual base selection")
	}
	ctx, cancel := operation(ctx)
	defer cancel()
	dir, release, err := c.repository(ctx, resolution.PR)
	if err != nil {
		return github.Resolution{}, err
	}
	defer release()
	refs, err := c.refs(ctx, dir)
	if err != nil {
		return github.Resolution{}, err
	}
	if refs["refs/sheldon/bases/"+branch] != sha {
		return github.Resolution{}, ErrRefreshRequired
	}
	if err := c.commit(ctx, dir, sha); err != nil {
		return github.Resolution{}, err
	}
	id, _ := resolution.PR.RepositoryID()
	return resolution.SelectBase([]github.BaseCandidate{{RepositoryID: id, Branch: branch, SHA: sha}}, branch, sha)
}

// fetch mutates only explicit managed refs without hooks, pruning or checkout.
func (c *Cache) fetch(ctx context.Context, dir string, pr githubpr.PullRequest, specs []string) error {
	args := []string{"fetch", "--no-tags", "--no-recurse-submodules", "--no-auto-maintenance", "--atomic", "--no-write-fetch-head", "--no-prune", "--no-prune-tags", "--refmap=", "--no-progress", "--", remoteURL(pr)}
	_, err := c.run(ctx, dir, append(args, specs...)...)
	return err
}

// refs rejects symbolic or malformed managed refs before allowing fetch writes.
func (c *Cache) refs(ctx context.Context, dir string) (map[string]string, error) {
	output, err := c.run(ctx, dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", "refs/sheldon/")
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "refs/sheldon/") || !githubpr.ValidBranch(strings.TrimPrefix(parts[0], "refs/")) || !githubpr.ValidCommit(parts[1]) || parts[2] != "" || refs[parts[0]] != "" {
			return nil, errors.New("unsafe managed Git refs; preserve cache before retrying")
		}
		refs[parts[0]] = parts[1]
	}
	return refs, nil
}

// commit verifies that a pinned object is a parseable commit without lazy replacement.
func (c *Cache) commit(ctx context.Context, dir, sha string) error {
	if !githubpr.ValidCommit(sha) {
		return errors.New("invalid commit identity")
	}
	typeName, err := c.run(ctx, dir, "cat-file", "-t", sha)
	if err != nil {
		return err
	}
	if string(typeName) != "commit\n" {
		return errors.New("fetched object is not a commit")
	}
	parsed, err := c.run(ctx, dir, "rev-parse", "--verify", "--end-of-options", sha+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(parsed)) != sha {
		return errors.New("fetched commit identity changed")
	}
	return nil
}

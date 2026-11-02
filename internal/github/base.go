// Package github resolves PR metadata through the user's existing GitHub CLI.
package github

import (
	"context"
	"errors"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// State distinguishes reported metadata from explicitly selected Git-only bases.
type State string

const (
	Resolved              State = "resolved"
	FallbackRequired      State = "fallback-required"
	BaseSelectionRequired State = "base-selection-required"
)

// Base is a pinned target branch, not a merge base or fetched-source assertion.
type Base struct {
	Branch string
	SHA    string
	Source githubpr.BaseSource
}

// Resolution retains PR identity and an explicit unresolved or resolved base.
type Resolution struct {
	PR      githubpr.PullRequest
	State   State
	Base    *Base
	HeadSHA string
	Failure *MetadataError
}

// BaseCandidate describes a commit branch supplied by the future managed fetch layer.
type BaseCandidate struct {
	RepositoryID string
	Branch       string
	SHA          string
}

// Resolve returns validated gh metadata or an explicit fallback requirement.
func Resolve(ctx context.Context, pr githubpr.PullRequest) (Resolution, error) {
	if err := pr.Validate(); err != nil {
		return Resolution{}, err
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	result, err := metadata(ctx, pr)
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return Resolution{}, ctx.Err()
	}
	var failure *MetadataError
	if !errors.As(err, &failure) {
		return Resolution{}, err
	}
	return Resolution{PR: pr, State: FallbackRequired, Failure: failure}, nil
}

// ChooseGitOnly requires explicit fallback consent without selecting any branch.
func (r Resolution) ChooseGitOnly() (Resolution, error) {
	if r.PR.Validate() != nil || r.State != FallbackRequired || r.Failure == nil || r.Base != nil || r.HeadSHA != "" {
		return Resolution{}, errors.New("Git-only fallback requires an unresolved metadata failure")
	}
	r.State = BaseSelectionRequired
	return r, nil
}

// SelectBase checks an explicit selection against freshly fetched branch candidates.
func (r Resolution) SelectBase(candidates []BaseCandidate, branch, sha string) (Resolution, error) {
	if r.PR.Validate() != nil || r.State != BaseSelectionRequired || r.Base != nil || r.HeadSHA != "" || r.Failure == nil {
		return Resolution{}, errors.New("choose Git-only fallback before selecting a base")
	}
	if !githubpr.ValidBranch(branch) || !githubpr.ValidCommit(sha) {
		return Resolution{}, errors.New("invalid base selection")
	}
	if len(candidates) == 0 {
		return Resolution{}, errors.New("no fetched base branches are available; fetch branches before selecting")
	}
	if len(candidates) > 1000 {
		return Resolution{}, errors.New("too many base candidates; narrow the fetched branch list")
	}
	repositoryID, _ := r.PR.RepositoryID()
	seen := make(map[string]bool, len(candidates))
	matched := false
	for _, candidate := range candidates {
		if candidate.RepositoryID != repositoryID || !githubpr.ValidBranch(candidate.Branch) || !githubpr.ValidCommit(candidate.SHA) || len(candidate.SHA) != len(candidates[0].SHA) || seen[candidate.Branch] {
			return Resolution{}, errors.New("invalid fetched base candidates; refresh branches before selecting")
		}
		seen[candidate.Branch] = true
		if candidate.Branch == branch && candidate.SHA == sha {
			matched = true
		}
	}
	if !matched {
		return Resolution{}, errors.New("base branch changed or is unavailable; refresh candidates and select again")
	}
	r.State = Resolved
	r.Base = &Base{Branch: branch, SHA: sha, Source: githubpr.UserSelectedBase}
	return r, nil
}

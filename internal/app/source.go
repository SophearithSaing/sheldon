package app

import (
	"context"
	"errors"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// sourceRegistry binds HTTP source requests to the existing writer and OS cache roots.
type sourceRegistry struct {
	*instance
	paths platform.Paths
}

// LoadSource resolves metadata or an explicitly selected Git-only base before preparing source.
func (s *sourceRegistry) LoadSource(ctx context.Context, pr githubpr.PullRequest, input httpapi.SourceRequest) (httpapi.SourceResult, error) {
	var resolution github.Resolution
	var err error
	switch input.Action {
	case "snapshot":
		if input.Revision == nil || input.Revision.Validate(pr.ID) != nil {
			return httpapi.SourceResult{}, errors.New("invalid pinned source request")
		}
		cache, err := s.openCache(s.paths)
		if err != nil {
			return httpapi.SourceResult{}, errors.Join(git.ErrSnapshotUnavailable, err)
		}
		prepared, err := cache.ReadPrepared(ctx, pr, *input.Revision)
		if err != nil {
			return httpapi.SourceResult{}, err
		}
		diff, err := cache.Diff(ctx, pr, prepared.Revision)
		if err != nil {
			return httpapi.SourceResult{}, errors.Join(git.ErrSnapshotUnavailable, err)
		}
		return httpapi.SourceResult{State: "ready", PR: pr, Diff: &diff}, nil
	case "refresh":
		if input.Revision == nil || input.Revision.Validate(pr.ID) != nil {
			return httpapi.SourceResult{}, errors.New("invalid refresh context")
		}
		if input.Revision.BaseSource == githubpr.UserSelectedBase {
			cache, err := s.openCache(s.paths)
			if err != nil {
				return httpapi.SourceResult{}, err
			}
			manual, _ := (github.Resolution{PR: pr, State: github.FallbackRequired, Failure: &github.MetadataError{Code: "explicit-git-only"}}).ChooseGitOnly()
			candidates, err := cache.FetchBases(ctx, manual)
			if err != nil {
				return httpapi.SourceResult{}, err
			}
			for _, candidate := range candidates {
				if candidate.Branch == input.Revision.BaseBranch {
					return s.LoadSource(ctx, pr, httpapi.SourceRequest{Action: "select-base", Branch: candidate.Branch, SHA: candidate.SHA})
				}
			}
			return httpapi.SourceResult{}, git.ErrRefreshRequired
		}
		fallthrough
	case "load":
		resolution, err = github.Resolve(ctx, pr)
		if err != nil {
			return httpapi.SourceResult{}, err
		}
		if resolution.State == github.FallbackRequired {
			return httpapi.SourceResult{State: "fallback-required", PR: pr}, nil
		}
	case "git-only", "select-base":
		resolution, err = (github.Resolution{PR: pr, State: github.FallbackRequired, Failure: &github.MetadataError{Code: "explicit-git-only"}}).ChooseGitOnly()
		if err != nil {
			return httpapi.SourceResult{}, err
		}
	default:
		return httpapi.SourceResult{}, errors.New("unsupported source action")
	}
	cache, err := s.openCache(s.paths)
	if err != nil {
		return httpapi.SourceResult{}, err
	}
	if input.Action == "git-only" {
		candidates, err := cache.FetchBases(ctx, resolution)
		if err != nil {
			return httpapi.SourceResult{}, err
		}
		result := httpapi.SourceResult{State: "base-selection-required", PR: pr, Candidates: []httpapi.SourceCandidate{}}
		for _, candidate := range candidates {
			result.Candidates = append(result.Candidates, httpapi.SourceCandidate{Branch: candidate.Branch, SHA: candidate.SHA})
		}
		return result, nil
	}
	if input.Action == "select-base" {
		resolution, err = cache.SelectBase(ctx, resolution, input.Branch, input.SHA)
		if err != nil {
			return httpapi.SourceResult{}, err
		}
	}
	snapshot, err := cache.Fetch(ctx, resolution)
	if err != nil {
		return httpapi.SourceResult{}, err
	}
	prepared, err := cache.Prepare(ctx, pr, snapshot)
	if err != nil {
		return httpapi.SourceResult{}, err
	}
	diff, err := cache.Diff(ctx, pr, prepared.Revision)
	if err != nil {
		return httpapi.SourceResult{}, err
	}
	return httpapi.SourceResult{State: "ready", PR: resolution.PR, Diff: &diff}, nil
}

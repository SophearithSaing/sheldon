package app

import (
	"context"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/semantic"
)

// GenerateSemantic admits explicit original-revision work after checking private predecessor health.
func (s *sourceRegistry) GenerateSemantic(ctx context.Context, prID string, revision githubpr.Revision, expected uint64) (semantic.Status, error) {
	if revision.Validate(prID) != nil {
		return semantic.Status{}, semantic.ErrSource
	}
	if _, err := s.ReadPR(prID); err != nil {
		return semantic.Status{}, semantic.ErrSource
	}
	if err := s.CheckSemantic(prID, expected); err != nil {
		return semantic.Status{}, err
	}
	if _, err := s.openExistingCache(s.paths); err != nil {
		return semantic.Status{}, semantic.ErrSource
	}
	s.mu.Lock()
	if !s.owned || s.closing || s.cache == nil || s.cacheRoot != s.paths.Cache || filepath.Dir(s.file.Name()) != s.paths.Data {
		s.mu.Unlock()
		return semantic.Status{}, semantic.ErrClosed
	}
	if s.semantics == nil {
		s.semantics = semantic.New(s.instance, s.cache)
	}
	r := s.semantics
	s.mu.Unlock()
	return r.Begin(ctx, prID, revision, expected)
}

// semanticRuntime reads current ownership without starting source or native work.
func (s *sourceRegistry) semanticRuntime() (*semantic.Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owned || s.closing {
		return nil, semantic.ErrClosed
	}
	if s.semantics == nil {
		return nil, semantic.ErrNotFound
	}
	return s.semantics, nil
}

// ReadSemanticGeneration observes one exact retained generation without dispatch.
func (s *sourceRegistry) ReadSemanticGeneration(prID, generationID string) (semantic.Status, error) {
	r, err := s.semanticRuntime()
	if err != nil {
		return semantic.Status{}, err
	}
	return r.Read(prID, generationID)
}

// CancelSemanticGeneration targets only the requested retained generation and original revision.
func (s *sourceRegistry) CancelSemanticGeneration(prID, generationID, revisionID string) (semantic.Status, error) {
	r, err := s.semanticRuntime()
	if err != nil {
		return semantic.Status{}, err
	}
	return r.Cancel(prID, generationID, revisionID)
}

// ReadSemanticView projects saved data without source/native startup and adds detached current feedback.
func (s *sourceRegistry) ReadSemanticView(prID string) (semantic.ReadResult, error) {
	saved, err := s.ReadSemantic(prID)
	if err != nil {
		return semantic.ReadResult{}, err
	}
	result, err := saved.Public()
	if err != nil {
		return semantic.ReadResult{}, err
	}
	s.mu.Lock()
	r := s.semantics
	s.mu.Unlock()
	if r != nil {
		result.Generation = r.Latest(prID)
	}
	return result, nil
}

package app

import (
	"context"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/platform"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// openQuizzes attaches independent lazy quiz execution to the writer's existing source cache.
func (i *instance) openQuizzes(paths platform.Paths) (*quiz.Runtime, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if paths.Validate() != nil || !i.owned || i.closing || filepath.Dir(i.file.Name()) != paths.Data {
		return nil, quiz.ErrClosed
	}
	if i.cache == nil || i.cacheRoot != paths.Cache {
		return nil, quiz.ErrSource
	}
	if i.quizzes == nil {
		i.quizzes = quiz.New(i, i.cache)
	}
	return i.quizzes, nil
}

// GenerateQuiz explicitly starts generation against a checked saved predecessor, not durable admission.
func (s *sourceRegistry) GenerateQuiz(ctx context.Context, prID string, revision githubpr.Revision, expected uint64) (quiz.Status, error) {
	if err := ctx.Err(); err != nil {
		return quiz.Status{}, err
	}
	if revision.Validate(prID) != nil {
		return quiz.Status{}, quiz.ErrSource
	}
	if _, err := s.ReadPR(prID); err != nil {
		return quiz.Status{}, quiz.ErrSource
	}
	if err := s.CheckQuiz(prID, expected); err != nil {
		return quiz.Status{}, err
	}
	if _, err := s.openExistingCache(s.paths); err != nil {
		return quiz.Status{}, quiz.ErrSource
	}
	runtime, err := s.openQuizzes(s.paths)
	if err != nil {
		return quiz.Status{}, err
	}
	return runtime.Begin(ctx, prID, revision, expected)
}

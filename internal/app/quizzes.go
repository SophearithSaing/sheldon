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

// ReadQuizView projects saved data independently of source/native availability.
func (s *sourceRegistry) ReadQuizView(prID string) (quiz.ReadResult, error) {
	saved, err := s.ReadQuiz(prID)
	if err != nil {
		return quiz.ReadResult{}, err
	}
	result, err := saved.Public()
	if err != nil {
		return quiz.ReadResult{}, err
	}
	s.mu.Lock()
	runtime := s.quizzes
	s.mu.Unlock()
	if runtime != nil {
		result.Generation = runtime.Latest(prID)
	}
	return result, nil
}

// CheckQuizAnswer grades the exact saved set under the same mutex used for replacement.
func (s *sourceRegistry) CheckQuizAnswer(prID, quizID, questionID, revisionID, choiceID string) (quiz.Checked, error) {
	s.quizStoreMu.Lock()
	defer s.quizStoreMu.Unlock()
	saved, err := s.readQuizLocked(prID)
	if err != nil {
		return quiz.Checked{}, err
	}
	return saved.Check(quizID, questionID, revisionID, choiceID)
}

// quizRuntime returns existing generation ownership without initializing source or a native service.
func (s *sourceRegistry) quizRuntime() (*quiz.Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owned || s.closing {
		return nil, quiz.ErrClosed
	}
	if s.quizzes == nil {
		return nil, quiz.ErrNotFound
	}
	return s.quizzes, nil
}

// ReadQuizGeneration observes one exact runtime attempt without dispatching input.
func (s *sourceRegistry) ReadQuizGeneration(prID, generationID string) (quiz.Status, error) {
	runtime, err := s.quizRuntime()
	if err != nil {
		return quiz.Status{}, err
	}
	return runtime.Read(prID, generationID)
}

// CancelQuizGeneration signals only the identified attempt and never starts native work.
func (s *sourceRegistry) CancelQuizGeneration(prID, generationID, revisionID string) (quiz.Status, error) {
	runtime, err := s.quizRuntime()
	if err != nil {
		return quiz.Status{}, err
	}
	return runtime.Cancel(prID, generationID, revisionID)
}

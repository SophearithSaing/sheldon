package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/platform"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// openQuestions attaches one lazy runtime to the app writer's already-open source cache.
func (i *instance) openQuestions(paths platform.Paths) (*question.Runtime, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if paths.Validate() != nil || !i.owned || i.closing || filepath.Dir(i.file.Name()) != paths.Data {
		return nil, errors.New("matching app writer ownership is required for questions")
	}
	if i.cache == nil || i.cacheRoot != paths.Cache {
		return nil, git.ErrQuestionSourceUnavailable
	}
	if i.questions == nil {
		i.questions = question.New(i, i.cache)
	}
	return i.questions, nil
}

// BeginQuestion admits an explicitly requested question without sending a native prompt.
func (s *sourceRegistry) BeginQuestion(ctx context.Context, prID string, input question.InitialRequest) (*question.Turn, error) {
	if err := input.Validate(prID, s.instance); err != nil {
		return nil, err
	}
	runtime, err := s.openQuestions(s.paths)
	if err != nil {
		return nil, err
	}
	return runtime.BeginQuestion(ctx, prID, input)
}

// BeginFollowup admits work on a known thread's original source and shared native session.
func (s *sourceRegistry) BeginFollowup(ctx context.Context, prID, threadID string, input question.FollowupRequest) (*question.Turn, error) {
	s.mu.Lock()
	runtime := s.questions
	available := s.owned && !s.closing && s.cacheRoot == s.paths.Cache && filepath.Dir(s.file.Name()) == s.paths.Data
	s.mu.Unlock()
	if !available || runtime == nil {
		return nil, question.ErrSessionUnavailable
	}
	return runtime.BeginFollowup(ctx, prID, threadID, input)
}

package app

import (
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// ListThreads reads saved navigation without preparing source, restoring runtime turns or starting inference.
func (s *sourceRegistry) ListThreads(prID string) (question.ThreadList, error) {
	saved, err := s.ReadQuestions(prID)
	if err != nil {
		return question.ThreadList{}, err
	}
	list := saved.PublicList()
	s.mu.Lock()
	runtime := s.questions
	s.mu.Unlock()
	if runtime != nil {
		for n, thread := range list.Threads {
			list.Threads[n].Persistence, _ = runtime.HistoryStatus(prID, thread.RevisionID, thread.ID)
		}
	}
	return list, nil
}

// ReadThread returns saved bytes and current runtime evidence, never newer unsaved answer text.
func (s *sourceRegistry) ReadThread(prID, threadID string) (question.ThreadDetail, error) {
	if !githubpr.ValidID(threadID) {
		return question.ThreadDetail{}, question.ErrNotFound
	}
	saved, err := s.ReadQuestions(prID)
	if err != nil {
		return question.ThreadDetail{}, err
	}
	detail, err := saved.PublicThread(threadID)
	if err != nil {
		return question.ThreadDetail{}, err
	}
	s.mu.Lock()
	runtime := s.questions
	s.mu.Unlock()
	if runtime != nil {
		persistence, status := runtime.HistoryStatus(prID, detail.RevisionID, threadID)
		detail.Persistence = persistence
		if detail.Session.State != "unavailable" {
			detail.Session = status
		}
	}
	return detail, nil
}

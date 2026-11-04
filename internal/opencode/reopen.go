package opencode

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"time"
)

// ReopenSession verifies an exact saved session and complete expected input history, without creation or inference.
// The caller must first verify the saved directory against its existing managed source.
func (s *Service) ReopenSession(ctx context.Context, record BindingRecord, prompts []string) (*Session, error) {
	if record.Validate() != nil {
		return nil, ErrRestricted
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if info, err := os.Lstat(record.Directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrRestricted
	}
	session := &Session{service: s}
	session.info.ID, session.info.Location.Directory, session.info.Model = record.SessionID, record.Directory, record.Model
	if err := s.ValidateSession(ctx, session); err != nil {
		return nil, err
	}
	if err := s.verifyPermissions(ctx, session); err != nil {
		return nil, err
	}
	if err := s.validateContinuation(ctx, session, prompts); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessionIDs[record.SessionID] {
		return nil, ErrRestricted
	}
	if s.sessionIDs == nil {
		s.sessionIDs = make(map[string]bool)
	}
	s.sessionIDs[record.SessionID] = true
	return session, nil
}

// validateContinuation proves each saved input's terminal history and an empty inbox without sending input.
func (s *Service) validateContinuation(ctx context.Context, session *Session, prompts []string) error {
	if err := s.ValidateSession(ctx, session); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	expected := make(map[string]bool)
	size := 0
	for _, prompt := range prompts {
		size += len(prompt)
		if prompt == "" || expected[prompt] || size > 8<<20 || len(expected) >= 1000 {
			return ErrProtocol
		}
		expected[prompt] = true
	}
	messages, err := s.history(ctx, session)
	if err != nil {
		return err
	}
	start := 0
	for n := 0; n <= len(messages); n++ {
		if n < len(messages) && messages[n].Type != "user" {
			continue
		}
		if n > start {
			segment := messages[start:n]
			if segment[0].Type != "user" || !expected[segment[0].UserText] {
				return ErrProtocol
			}
			result, terminal, err := executionResult(nil, segment, segment[0].ID, segment[0].UserText)
			if err != nil || !terminal || !result.Idle || result.Err != nil {
				return ErrProtocol
			}
			delete(expected, segment[0].UserText)
		}
		start = n
	}
	if len(expected) != 0 || !idleHistory(messages) {
		return ErrProtocol
	}
	var inbox struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := s.call(ctx, "GET", "/api/session/"+url.PathEscape(session.info.ID)+"/inbox", nil, &inbox); err != nil {
		return err
	}
	if inbox.Data == nil || len(inbox.Data) != 0 {
		return ErrProtocol
	}
	return s.Err()
}

package opencode

import (
	"context"
	"net/url"
	"strings"
)

type historyMessage struct {
	ID, Type, Outcome, UserText, Text string
}

// history follows the pinned protocol's opaque boundary cursors and retains visible fields only.
func (s *Service) history(ctx context.Context, session *Session) ([]historyMessage, error) {
	all := []historyMessage{}
	ids, cursors := map[string]historyMessage{}, map[string]bool{}
	query := "limit=100&order=asc"
	total := 0
	for range 16 {
		var page struct {
			Data []struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Outcome string `json:"outcome"`
				Text    string `json:"text"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"data"`
			Cursor struct {
				Next *string `json:"next"`
			} `json:"cursor"`
		}
		if err := s.call(ctx, "GET", "/api/session/"+url.PathEscape(session.info.ID)+"/message?"+query, nil, &page); err != nil {
			return nil, err
		}
		if page.Data == nil {
			return nil, ErrProtocol
		}
		for _, item := range page.Data {
			if !validNativeID(item.ID) || item.Type == "" {
				return nil, ErrProtocol
			}
			message := historyMessage{ID: item.ID, Type: item.Type, Outcome: item.Outcome}
			if item.Type == "user" {
				message.UserText = item.Text
			}
			if item.Type == "assistant" {
				for _, part := range item.Content {
					if part.Type == "text" {
						message.Text += part.Text
					}
				}
			}
			if previous, ok := ids[item.ID]; ok {
				if previous != message {
					return nil, ErrProtocol
				}
				continue
			}
			ids[item.ID] = message
			total += len(message.Text) + len(message.UserText)
			if total > 8<<20 || len(all) >= 1000 {
				return nil, ErrProtocol
			}
			all = append(all, message)
		}
		if len(page.Data) == 0 || page.Cursor.Next == nil || *page.Cursor.Next == "" {
			return all, nil
		}
		cursor := *page.Cursor.Next
		if cursors[cursor] || len(cursor) > 4096 {
			return nil, ErrProtocol
		}
		cursors[cursor] = true
		query = "limit=100&cursor=" + url.QueryEscape(cursor)
	}
	return nil, ErrProtocol
}

// idleHistory requires a terminal boundary before reusing a session with prior conversation.
func idleHistory(messages []historyMessage) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Type {
		case "idle":
			return messages[i].Outcome == "succeeded" || messages[i].Outcome == "failed" || messages[i].Outcome == "interrupted"
		case "user", "assistant":
			return false
		}
	}
	return true
}

// executionResult requires one matching new input and a trailing native idle outcome after it.
func executionResult(baseline, messages []historyMessage, inputID, prompt string) (Result, bool, error) {
	if len(messages) < len(baseline) {
		return Result{}, false, ErrProtocol
	}
	for i := range baseline {
		if baseline[i] != messages[i] {
			return Result{}, false, ErrProtocol
		}
	}
	tail := messages[len(baseline):]
	user, idle := -1, -1
	for i, message := range tail {
		if message.Type == "user" {
			if user != -1 || (inputID != "" && message.ID != inputID) || message.UserText != prompt {
				return Result{}, false, ErrProtocol
			}
			user = i
		}
		if message.Type == "idle" {
			if user < 0 || idle >= 0 {
				return Result{}, false, ErrProtocol
			}
			idle = i
		}
	}
	if user < 0 || idle < 0 {
		return Result{}, false, nil
	}
	if idle != len(tail)-1 {
		return Result{}, false, ErrProtocol
	}
	text := ""
	for _, message := range tail[user+1 : idle] {
		if message.Type == "assistant" {
			text += message.Text
		}
	}
	result := Result{Text: text, Idle: true}
	if len(text) > maxVisibleBytes || strings.ContainsRune(text, 0) {
		result.Text, result.State, result.Err = "", "failed", ErrAnswerLimit
		return result, true, nil
	}
	switch tail[idle].Outcome {
	case "succeeded":
		result.State = "completed"
		if strings.TrimSpace(text) == "" {
			result.State, result.Err = "failed", ErrProtocol
		}
	case "failed":
		result.State = "failed"
	case "interrupted":
		result.State = "interrupted"
	default:
		return Result{}, false, ErrProtocol
	}
	return result, true, nil
}

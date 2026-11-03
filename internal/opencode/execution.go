package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const maxVisibleBytes = 256 << 10

var ErrAnswerLimit = errors.New("native visible answer exceeds bounded output limits")

type nativeEvent struct {
	id, kind, messageID, text string
}

type subscription struct {
	directory string
	events    chan nativeEvent
	failure   chan error
}

// Result contains only visible text and a confirmed outcome, never raw native diagnostics.
// Idle is false when execution cannot be proved terminal; that session must remain blocked.
type Result struct {
	Text  string
	State string
	Idle  bool
	Err   error
}

// deliver filters native activity into bounded managed-session queues without forwarding raw payloads.
func (s *Service) deliver(data []byte) error {
	var envelope struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Location struct {
			Directory string `json:"directory"`
		} `json:"location"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return ErrProtocol
	}
	if envelope.Type != "session.text.delta" && !strings.HasPrefix(envelope.Type, "session.execution.") {
		return nil
	}
	var route struct {
		SessionID          string `json:"sessionID"`
		AssistantMessageID string `json:"assistantMessageID"`
	}
	if json.Unmarshal(envelope.Data, &route) != nil {
		return ErrProtocol
	}
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	sub := s.listeners[route.SessionID]
	if sub == nil {
		return nil
	}
	if envelope.Location.Directory != "" && envelope.Location.Directory != sub.directory {
		select {
		case sub.failure <- ErrProtocol:
		default:
		}
		return nil
	}
	event := nativeEvent{id: envelope.ID, kind: envelope.Type, messageID: route.AssistantMessageID}
	if envelope.Type == "session.text.delta" {
		var text struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(envelope.Data, &text) != nil || !utf8.ValidString(text.Delta) || len(text.Delta) > 64<<10 || strings.ContainsRune(text.Delta, 0) {
			select {
			case sub.failure <- ErrAnswerLimit:
			default:
			}
			return nil
		}
		event.text = text.Delta
	}
	select {
	case sub.events <- event:
	default:
		select {
		case sub.failure <- ErrStreamLost:
		default:
		}
	}
	return nil
}

// subscribe registers observation before dispatch and rejects competing native use of one session.
func (s *Service) subscribe(session *Session) (*subscription, error) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if session == nil || session.service != s || s.feedErr != nil || s.listeners[session.info.ID] != nil {
		return nil, ErrRestricted
	}
	sub := &subscription{directory: session.info.Location.Directory, events: make(chan nativeEvent, 32), failure: make(chan error, 1)}
	s.listeners[session.info.ID] = sub
	return sub, nil
}

// unsubscribe removes only this execution's listener; it never interrupts or redispatches input.
func (s *Service) unsubscribe(session *Session, sub *subscription) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if s.listeners[session.info.ID] == sub {
		delete(s.listeners, session.info.ID)
	}
}

// Execute sends one prompt at most once and reconciles only that input's history boundary.
// stop requests targeted interruption; emit receives cumulative escaped-text-ready content only.
func (s *Service) Execute(ctx context.Context, session *Session, prompt string, stop <-chan struct{}, emit func(string)) Result {
	if err := s.Err(); err != nil {
		return Result{State: "interrupted", Err: err}
	}
	if err := s.ValidateSession(ctx, session); err != nil {
		return Result{State: "interrupted", Err: err}
	}
	sub, err := s.subscribe(session)
	if err != nil {
		return Result{State: "interrupted", Err: err}
	}
	defer s.unsubscribe(session, sub)
	baseline, err := s.history(ctx, session)
	if err != nil || !idleHistory(baseline) {
		return Result{State: "interrupted", Err: ErrProtocol}
	}
	var inbox struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := s.call(ctx, "GET", "/api/session/"+url.PathEscape(session.info.ID)+"/inbox", nil, &inbox); err != nil || inbox.Data == nil || len(inbox.Data) != 0 {
		return Result{State: "interrupted", Err: ErrProtocol}
	}
	select {
	case <-stop:
		return Result{State: "interrupted", Idle: true}
	default:
	}
	if err := ctx.Err(); err != nil {
		return Result{State: "interrupted", Idle: true, Err: err}
	}
	var ack struct {
		Data struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionID"`
		} `json:"data"`
	}
	dispatchErr := s.call(ctx, "POST", "/api/session/"+url.PathEscape(session.info.ID)+"/prompt", map[string]any{"text": prompt, "resume": true}, &ack)
	if !validNativeID(ack.Data.ID) || ack.Data.SessionID != session.info.ID {
		ack.Data.ID = ""
		dispatchErr = ErrProtocol
	}
	text := ""
	seen := make(map[string]string)
	oldMessages := make(map[string]bool)
	for _, message := range baseline {
		oldMessages[message.ID] = true
	}
	stopRequested := false
	feed := s.feedDone
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var fault error
	if dispatchErr != nil {
		fault = dispatchErr
	}
	for {
		if fault != nil {
			return s.recoverExecution(session, baseline, ack.Data.ID, prompt, text, fault)
		}
		select {
		case event := <-sub.events:
			if event.kind == "session.text.delta" {
				if oldMessages[event.messageID] || !validNativeID(event.messageID) || !validNativeID(event.id) {
					continue
				}
				signature := event.messageID + "\x00" + event.text
				if prior, ok := seen[event.id]; ok {
					if prior != signature {
						fault = ErrProtocol
					}
					continue
				}
				if len(seen) >= 4096 || len(text)+len(event.text) > maxVisibleBytes {
					fault = ErrAnswerLimit
					continue
				}
				seen[event.id] = signature
				text += event.text
				emit(text)
			}
		case err := <-sub.failure:
			fault = err
		case <-feed:
			feed = nil
			fault = ErrStreamLost
		case <-stop:
			stop = nil
			stopRequested = true
			if err := s.interrupt(ctx, session); err != nil {
				fault = err
			}
		case <-ctx.Done():
			fault = ctx.Err()
		case <-tick.C:
			messages, err := s.history(ctx, session)
			if err != nil {
				fault = err
				continue
			}
			result, terminal, err := executionResult(baseline, messages, ack.Data.ID, prompt)
			if err != nil {
				fault = err
				continue
			}
			if terminal {
				if result.Err != nil {
					result.Text = text
				}
				return result
			}
			if stopRequested {
				return s.recoverExecution(session, baseline, ack.Data.ID, prompt, text, nil)
			}
		}
	}
}

// interrupt requests only this managed native session; its boolean response is not terminal proof.
func (s *Service) interrupt(ctx context.Context, session *Session) error {
	var result struct {
		Interrupted *bool `json:"interrupted"`
	}
	if err := s.call(ctx, "POST", "/api/session/"+url.PathEscape(session.info.ID)+"/interrupt?resume=false", nil, &result); err != nil {
		return err
	}
	if result.Interrupted == nil {
		return ErrProtocol
	}
	return nil
}

// recoverExecution interrupts and performs bounded read-only reconciliation without sending input again.
func (s *Service) recoverExecution(session *Session, baseline []historyMessage, inputID, prompt, partial string, fault error) Result {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	_ = s.interrupt(ctx, session)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		messages, err := s.history(ctx, session)
		if err == nil {
			result, terminal, parseErr := executionResult(baseline, messages, inputID, prompt)
			if terminal && parseErr == nil {
				if errors.Is(fault, ErrAnswerLimit) {
					result.State, result.Text, result.Err = "failed", partial, ErrAnswerLimit
				} else if result.Err != nil {
					result.Text = partial
				}
				return result
			}
		}
		select {
		case <-ctx.Done():
			return Result{State: "interrupted", Text: partial, Err: ErrStreamLost}
		case <-tick.C:
		}
	}
}

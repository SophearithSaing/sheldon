package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strings"
	"time"
	"unicode/utf8"
)

const maxFrame = 1 << 20

// guard routes only managed visible events and fails closed on unattended safety requests.
func (s *Service) guard(connected chan<- error) {
	defer close(s.guardDone)
	announced := false
	err := s.watch(func(data []byte) error {
		var event struct {
			Type string `json:"type"`
		}
		if !utf8.Valid(data) || json.Unmarshal(data, &event) != nil || event.Type == "" {
			return ErrProtocol
		}
		if !announced {
			if event.Type != "server.connected" {
				return ErrProtocol
			}
			announced = true
			connected <- nil
		}
		if event.Type == "permission.asked" || event.Type == "form.created" {
			return ErrRestricted
		}
		return s.deliver(data)
	})
	if err == nil {
		err = ErrStreamLost
	}
	if !announced {
		connected <- err
	}
	s.feedMu.Lock()
	s.feedErr = err
	close(s.feedDone)
	s.feedMu.Unlock()
	if errors.Is(err, ErrRestricted) || errors.Is(err, ErrProtocol) {
		s.cancel(err)
	} else {
		timer := time.AfterFunc(30*time.Second, func() { s.cancel(ErrStreamLost) })
		context.AfterFunc(s.ctx, func() { timer.Stop() })
	}
}

// watch consumes the owned native feed without replay, automatic reconnect or raw payload forwarding.
func (s *Service) watch(emit func([]byte) error) error {
	r, _, cleanup, err := s.request(s.ctx, "GET", "/api/event", nil)
	if err != nil {
		return err
	}
	defer cleanup()
	defer r.Body.Close()
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" {
		return ErrProtocol
	}
	return readSSE(r.Body, emit)
}

// readSSE bounds lines and complete frames, including comments and otherwise ignored fields.
func readSSE(reader io.Reader, emit func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	var data []byte
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				if err := emit(bytes.TrimSuffix(data, []byte{'\n'})); err != nil {
					return err
				}
			}
			data, size = nil, 0
			continue
		}
		size += len(line) + 1
		if size > maxFrame {
			return ErrProtocol
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			if value == "effect/http-api/stream/failure" {
				return ErrStreamLost
			}
		case "data":
			data = append(data, value...)
			data = append(data, '\n')
		}
	}
	if scanner.Err() != nil {
		return errors.Join(ErrStreamLost, ErrProtocol)
	}
	return ErrStreamLost
}

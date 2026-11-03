package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"
)

const maxResponse = 8 << 20

// validEndpoint accepts only a canonical explicit IPv4 loopback endpoint from the owned child.
func validEndpoint(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535 && value == "http://127.0.0.1:"+strconv.Itoa(port)
}

// request opens an authenticated owned-service request and closes it on either caller or service cancellation.
func (s *Service) request(ctx context.Context, method, path string, body any) (*http.Response, context.Context, context.CancelFunc, error) {
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	if err := context.Cause(s.ctx); err != nil {
		cleanup()
		return nil, combined, cleanup, err
	}
	var data bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&data).Encode(body); err != nil || data.Len() > maxResponse {
			cleanup()
			return nil, combined, cleanup, ErrProtocol
		}
	}
	req, err := http.NewRequestWithContext(combined, method, s.base+path, &data)
	if err != nil {
		cleanup()
		return nil, combined, cleanup, ErrProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("opencode", s.password)
	response, err := s.client.Do(req)
	if err != nil {
		err = safeError(combined, ErrUnavailable)
		cleanup()
		return nil, combined, cleanup, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		cleanup()
		return nil, combined, cleanup, ErrUnavailable
	}
	return response, combined, cleanup, nil
}

// call bounds a finite JSON exchange; no response diagnostic body is exposed on failure.
func (s *Service) call(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, requestCtx, cleanup, err := s.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer cleanup()
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, maxResponse+1))
	if err != nil {
		return safeError(requestCtx, ErrUnavailable)
	}
	if len(data) > maxResponse || !utf8.Valid(data) {
		return ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrProtocol
	}
	return nil
}

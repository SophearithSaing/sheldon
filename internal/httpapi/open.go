package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// SetOpenURL records the latest validated PR request without loading it.
func (s *Server) SetOpenURL(raw string) error {
	u, err := githubpr.NormalizeURL(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.openURL = u
	s.mu.Unlock()
	return nil
}

// open accepts one bounded PR-open intent; newer intents replace older ones.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "JSON body required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if end := decoder.Decode(&extra); !errors.Is(end, io.EOF) {
			err = errors.New("unexpected request data")
			if end != nil {
				err = end
			}
		}
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "open body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid open body")
		}
		return
	}
	if s.SetOpenURL(input.URL) != nil {
		writeError(w, http.StatusBadRequest, "invalid GitHub PR URL")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// openState reports the latest request without claiming the PR has been loaded.
func (s *Server) openState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	u := s.openURL
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"url": u})
}

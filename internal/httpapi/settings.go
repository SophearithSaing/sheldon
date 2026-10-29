package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/SophearithSaing/sheldon/internal/settings"
)

// settingsState exposes an honest unconfirmed scaffold without reading/writing settings.
func (s *Server) settingsState(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "settings do not accept query parameters")
		return
	}
	writeJSON(w, http.StatusOK, settings.Scaffold())
}

// confirmSettings validates the proposed contract without simulating persistence.
func (s *Server) confirmSettings(w http.ResponseWriter, r *http.Request) {
	var input settings.Confirmation
	if !decodeSettingsBody(w, r, &input) {
		return
	}
	if input.Validate() != nil {
		writeError(w, http.StatusBadRequest, "invalid settings or missing explicit confirmation")
		return
	}
	writeError(w, http.StatusNotImplemented, "saving preferences is not implemented; nothing was saved or applied")
}

// modelDiscovery returns explicit unknown/unsupported metadata without native calls.
func (s *Server) modelDiscovery(w http.ResponseWriter, r *http.Request) {
	h := settings.Harness(r.PathValue("id"))
	if !h.Valid() {
		writeError(w, http.StatusNotFound, "unknown harness")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "model discovery does not accept targets or parameters")
		return
	}
	writeJSON(w, http.StatusOK, settings.Undiscovered(h))
}

// testAnswer rejects real execution until safe cancellable adapter runs exist.
// No run ID or success/admission response is created, so clients must not redispatch.
func (s *Server) testAnswer(w http.ResponseWriter, r *http.Request) {
	var input settings.TestRequest
	if !decodeSettingsBody(w, r, &input) {
		return
	}
	if input.Validate() != nil {
		writeError(w, http.StatusBadRequest, "invalid test selection, request identity or quota confirmation")
		return
	}
	writeError(w, http.StatusNotImplemented, "test answers are not implemented; no run was admitted and no model request was sent")
}

// decodeSettingsBody accepts bounded strict JSON without echoing untrusted field values.
func decodeSettingsBody(w http.ResponseWriter, r *http.Request, input any) bool {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "settings requests do not accept query parameters")
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "JSON body required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(input)
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
			writeError(w, http.StatusRequestEntityTooLarge, "settings request too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid settings request")
		}
		return false
	}
	return true
}

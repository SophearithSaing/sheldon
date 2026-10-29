package httpapi

import (
	"net/http"

	"github.com/SophearithSaing/sheldon/internal/environment"
)

// environment reports bounded, noninteractive CLI metadata to an authenticated caller.
// Only one scan runs at a time; client disconnect or server shutdown cancels its children.
func (s *Server) environment(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "environment checks do not accept parameters")
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	select {
	case s.environmentBusy <- struct{}{}:
		defer func() { <-s.environmentBusy }()
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "an environment check is already running; retry when it finishes")
		return
	}
	report, err := environment.Inspect(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "environment check could not finish; retry and check OS temporary-directory access")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

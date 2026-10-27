package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// fixtureEvents streams labeled fixture heartbeats until cancellation or shutdown.
func (s *Server) fixtureEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	defer controller.SetWriteDeadline(time.Time{})
	if err := writeFixtureEvent(w, controller, "connected"); err != nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := writeFixtureEvent(w, controller, "heartbeat"); err != nil {
				return
			}
		}
	}
}

// writeFixtureEvent writes and flushes one fixture event under a write deadline.
func writeFixtureEvent(w http.ResponseWriter, controller *http.ResponseController, status string) error {
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]string{"fixture": status})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: fixture\ndata: %s\n\n", data); err != nil {
		return err
	}
	return controller.Flush()
}

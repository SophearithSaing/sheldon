// Package httpapi serves Sheldon's protected loopback HTTP API.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// Server owns a loopback listener and an in-memory API bearer credential.
// Construct it with Listen, call Serve, and release it with Shutdown.
type Server struct {
	listener  net.Listener
	http      *http.Server
	mux       *http.ServeMux
	origin    string
	authority string
	apiToken  string
	stop      context.CancelFunc
}

// Listen opens a numeric loopback address and prepares the protected API.
// An empty address selects 127.0.0.1:0; port zero lets the OS choose a free port.
func Listen(address string) (*Server, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("server requires a numeric loopback address and port")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || ip.Is4In6() || ip.Zone() != "" {
		return nil, errors.New("server must bind to a numeric loopback address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 || strconv.Itoa(n) != port {
		return nil, errors.New("invalid server port")
	}
	network := "tcp4"
	if ip.Is6() {
		network = "tcp6"
	}
	listener, err := net.Listen(network, net.JoinHostPort(ip.String(), port))
	if err != nil {
		return nil, err
	}
	s := &Server{listener: listener, mux: http.NewServeMux(), authority: listener.Addr().String()}
	s.origin = "http://" + s.authority
	s.apiToken, err = newSecret()
	if err != nil {
		listener.Close()
		return nil, err
	}
	lifetime, stop := context.WithCancel(context.Background())
	s.stop = stop
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/fixture/events", s.fixtureEvents)
	s.http = &http.Server{
		Handler:           http.HandlerFunc(s.handle),
		BaseContext:       func(net.Listener) context.Context { return lifetime },
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		// No access/request logging, and no raw request data in server error logs.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	return s, nil
}

// URL returns the server's canonical HTTP origin without credentials.
func (s *Server) URL() string { return s.origin }

// String returns the server origin without exposing local credentials.
func (s *Server) String() string {
	return fmt.Sprintf("Sheldon HTTP server at %s (credentials withheld)", s.origin)
}

// GoString returns the same credential-safe representation for fmt's %#v format.
func (s *Server) GoString() string { return s.String() }

// APIToken returns the per-start bearer credential used by all API clients.
func (s *Server) APIToken() string { return s.apiToken }

// Serve accepts requests until Shutdown is called or the listener fails.
func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	s.stop()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown cancels active streams, drains requests and closes the listener.
// If the deadline expires, remaining connections are forcibly closed.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stop()
	err := s.http.Shutdown(ctx)
	if err != nil {
		s.http.Close()
	}
	// Also release a listener when Serve was never started.
	s.listener.Close()
	return err
}

// health reports availability to an authenticated client.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// newSecret generates a 256-bit, URL-safe local credential.
func newSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// writeJSON writes a JSON response without caching it.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

// writeError writes a fixed, public error message without request details.
func writeError(w http.ResponseWriter, status int, message string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(w, status, map[string]string{"error": message})
}

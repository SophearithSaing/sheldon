// Package opencode owns a restricted, version-specific native service, not provider authentication.
package opencode

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/SophearithSaing/sheldon/internal/process"
)

// SupportedVersion pins the spike's audited native startup and permission contract.
const SupportedVersion = "2.1.4"

var (
	ErrUnavailable      = errors.New("restricted OpenCode service unavailable; check the supported installation externally")
	ErrRestricted       = errors.New("OpenCode investigation restrictions could not be verified; execution is unavailable")
	ErrProtocol         = errors.New("unsupported or malformed OpenCode protocol response")
	ErrStreamLost       = errors.New("OpenCode safety stream lost; execution is unavailable, do not replay prompts")
	ErrModelUnavailable = errors.New("OpenCode default model is unavailable or unsupported; configure it externally, no fallback was selected")
)

// Service owns only its private child, local transport and temporary startup directory.
type Service struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	child      *process.Process
	transport  *http.Transport
	client     *http.Client
	base       string
	password   string
	directory  string
	closed     sync.Once
	readDone   chan struct{}
	guardDone  chan struct{}
	closeErr   error
	sessionMu  sync.Mutex
	sessionIDs map[string]bool
}

// Start verifies and starts the installed native executable without contacting a shared service.
// Its context owns the returned service lifetime; no model inference is performed here.
func Start(ctx context.Context) (*Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, name := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT"} {
		if os.Getenv(name) != "" {
			return nil, ErrRestricted
		}
	}
	executable, err := exec.LookPath("opencode")
	ext := strings.ToLower(filepath.Ext(executable))
	if err != nil || !filepath.IsAbs(executable) || ext == ".cmd" || ext == ".bat" || ext == ".ps1" || runtime.GOOS != "windows" && ext == ".exe" || !filepath.IsAbs(os.TempDir()) {
		return nil, ErrUnavailable
	}
	directory, err := os.MkdirTemp("", "sheldon-opencode-")
	if err != nil {
		return nil, ErrUnavailable
	}
	lifetime, cancel := context.WithCancelCause(ctx)
	s := &Service{ctx: lifetime, cancel: cancel, directory: directory}
	success := false
	defer func() {
		if !success {
			s.Close()
		}
	}()
	startup, stop := context.WithTimeout(lifetime, 30*time.Second)
	defer stop()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, ErrUnavailable
	}
	s.password = hex.EncodeToString(secret)
	config, _ := json.Marshal(investigationConfig())
	env := []string{"OPENCODE_PASSWORD=" + s.password, "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_CONFIG_PROJECT_DISABLE=1", "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_CONFIG_CONTENT=" + string(config)}
	version, err := process.Run(startup, process.Command{Name: executable, Args: []string{"--version"}, Dir: directory, Env: env, OutputLimit: 64 << 10, GracePeriod: 100 * time.Millisecond})
	if err != nil || strings.TrimSpace(string(version.Stdout)) != "opencode v"+SupportedVersion {
		return nil, safeError(startup, ErrUnavailable)
	}
	s.child, err = process.Start(lifetime, process.Command{Name: executable, Args: []string{"serve", "--stdio", "--hostname", "127.0.0.1", "--port", "0"}, Dir: directory, Env: env, OutputLimit: 1 << 20, GracePeriod: 100 * time.Millisecond})
	if err != nil {
		return nil, ErrUnavailable
	}
	ready := make(chan string, 1)
	s.readDone = make(chan struct{})
	go s.readAnnouncement(ready)
	go func() {
		_, err := s.child.Wait()
		if errors.Is(err, process.ErrOutputLimit) {
			s.cancel(errors.Join(ErrUnavailable, process.ErrOutputLimit))
		} else {
			s.cancel(ErrUnavailable)
		}
	}()
	select {
	case line := <-ready:
		var announcement struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(line), &announcement) != nil || !validEndpoint(announcement.URL) {
			return nil, ErrProtocol
		}
		s.base = announcement.URL
	case <-startup.Done():
		return nil, safeError(startup, ErrUnavailable)
	}
	s.transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10}
	s.client = &http.Client{Transport: s.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var info struct {
		Version string `json:"version"`
	}
	if err := s.call(startup, "GET", "/api/info", nil, &info); err != nil {
		return nil, err
	}
	if info.Version != SupportedVersion {
		return nil, ErrProtocol
	}
	connected := make(chan error, 1)
	s.guardDone = make(chan struct{})
	go s.guard(connected)
	select {
	case err := <-connected:
		if err != nil {
			return nil, err
		}
	case <-startup.Done():
		return nil, safeError(startup, ErrStreamLost)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	success = true
	go func() { <-lifetime.Done(); s.Close() }()
	return s, nil
}

// readAnnouncement bounds the first stdout frame and drains subsequent native diagnostics privately.
func (s *Service) readAnnouncement(ready chan<- string) {
	defer close(s.readDone)
	scanner := bufio.NewScanner(s.child.Stdout)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	if scanner.Scan() {
		ready <- scanner.Text()
		_, _ = io.Copy(io.Discard, s.child.Stdout)
	} else {
		s.cancel(ErrProtocol)
	}
}

// Err reports a fixed terminal service failure without native diagnostics or credentials.
func (s *Service) Err() error { return context.Cause(s.ctx) }

// Close cancels owned activity and removes only this service's temporary startup directory.
func (s *Service) Close() error {
	s.closed.Do(func() {
		s.cancel(context.Canceled)
		if s.child != nil {
			s.child.Close()
			<-s.readDone
		}
		if s.transport != nil {
			s.transport.CloseIdleConnections()
		}
		if s.guardDone != nil {
			<-s.guardDone
		}
		if err := os.Remove(s.directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.closeErr = errors.New("cannot remove owned OpenCode startup directory; existing contents were preserved")
		}
	})
	return s.closeErr
}

// String withholds native endpoints, filesystem paths and local credentials.
func (s *Service) String() string { return "restricted OpenCode service (native details withheld)" }

// GoString keeps diagnostic formatting credential-safe.
func (s *Service) GoString() string { return s.String() }

// safeError preserves cancellation/deadline evidence without exposing native response content.
func safeError(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(fallback, err)
	}
	return fallback
}

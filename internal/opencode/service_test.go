package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

var fixtureBin string

func TestMain(m *testing.M) {
	var err error
	fixtureBin, err = os.MkdirTemp("", "sheldon-native-fixture-")
	if err != nil {
		os.Exit(1)
	}
	name := "opencode"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(fixtureBin, name), "./testdata/opencode")
	if data, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "offline native fixture build: %v %s", err, data)
		os.RemoveAll(fixtureBin)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(fixtureBin)
	os.Exit(code)
}

type auditEntry struct {
	Args           []string       `json:"args"`
	Directory      string         `json:"directory"`
	Restricted     bool           `json:"restricted"`
	PasswordLength int            `json:"passwordLength"`
	AuthPreserved  bool           `json:"authPreserved"`
	Config         map[string]any `json:"config"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	Body           string         `json:"body"`
}

func fixture(t *testing.T, mode string) string {
	t.Helper()
	audit := filepath.Join(t.TempDir(), "native-audit.jsonl")
	for name, value := range map[string]string{"PATH": fixtureBin, "OPENCODE_CONFIG": "", "OPENCODE_CONFIG_DIR": "", "OPENCODE_CONFIG_CONTENT": "", "SHELDON_NATIVE_MODE": mode, "SHELDON_NATIVE_AUDIT": audit, "SHELDON_SYNTHETIC_AUTH": "fixture-route"} {
		t.Setenv(name, value)
	}
	return audit
}

func readAudit(t *testing.T, path string) []auditEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []auditEntry
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var entry auditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestOwnedServiceAndRestrictedSession(t *testing.T) {
	audit := fixture(t, "")
	originalConfig := os.Getenv("OPENCODE_CONFIG_CONTENT")
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(lifetime)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	directory := t.TempDir()
	session, err := s.CreateSession(lifetime, directory)
	if err != nil || session == nil || session.Model() != (Model{ProviderID: "fixture", ID: "chat"}) {
		t.Fatalf("restricted session: %v", err)
	}
	if err := s.ValidateSession(lifetime, session); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{s, session} {
		encoded, _ := json.Marshal(value)
		formatted := fmt.Sprintf("%v %+v %#v %s", value, value, value, encoded)
		for _, secret := range []string{s.password, s.base, session.info.ID, directory} {
			if strings.Contains(formatted, secret) {
				t.Fatal("native transport/session details exposed through formatting or JSON")
			}
		}
	}
	entries := readAudit(t, audit)
	config, _ := json.Marshal(investigationConfig())
	var expected map[string]any
	json.Unmarshal(config, &expected)
	for _, entry := range entries {
		if len(entry.Args) > 0 {
			if !entry.Restricted || !entry.AuthPreserved || entry.PasswordLength != 64 || !reflect.DeepEqual(entry.Config, expected) || !filepath.IsAbs(entry.Directory) || entry.Directory == directory {
				t.Fatal("native startup policy/environment/cwd mismatch")
			}
			continue
		}
		switch entry.Path {
		case "/api/info", "/api/event", "/api/model/default", "/api/session", "/api/session/ses_fixture", "/api/session/ses_fixture/permission":
		default:
			t.Fatalf("unexpected native operation: %s", entry.Path)
		}
		if strings.Contains(entry.Body, "prompt") || strings.Contains(entry.Path, "credential") || strings.Contains(entry.Path, "connect") {
			t.Fatal("setup invoked inference/authentication")
		}
		if entry.Path == "/api/model/default" && entry.Directory != directory {
			t.Fatal("default model resolved in the wrong worktree")
		}
	}
	if !reflect.DeepEqual(entries[0].Args, []string{"--version"}) || !reflect.DeepEqual(entries[1].Args, []string{"serve", "--stdio", "--hostname", "127.0.0.1", "--port", "0"}) {
		t.Fatal("unexpected process argv")
	}
	if os.Getenv("OPENCODE_CONFIG_CONTENT") != originalConfig || os.Getenv("SHELDON_SYNTHETIC_AUTH") != "fixture-route" {
		t.Fatal("parent configuration/environment changed")
	}
	cancel()
	s.Close()
	if _, err := os.Stat(s.directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned startup directory not removed")
	}
	address := strings.TrimPrefix(s.base, "http://")
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("owned listener survived shutdown")
	}
	if _, err := s.CreateSession(context.Background(), directory); err == nil {
		t.Fatal("closed service admitted a new session")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStartupFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, mode := range []string{"version-wrong", "version-fail", "version-overflow", "version-hang", "early-exit", "bad-announcement", "oversize-announcement", "foreign-endpoint", "announcement-hang", "info-wrong", "http-redirect", "bad-auth", "event-content-type"} {
		t.Run(mode, func(t *testing.T) {
			audit := fixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			s, err := Start(ctx)
			if err == nil || s != nil {
				if s != nil {
					s.Close()
				}
				t.Fatal("startup failure returned a usable service")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.invalid") || strings.Contains(err.Error(), "native-audit") {
				t.Fatal("raw native diagnostics leaked")
			}
			for _, entry := range readAudit(t, audit) {
				if len(entry.Args) != 0 {
					if _, err := os.Stat(entry.Directory); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("failed startup left its directory")
					}
				}
			}
		})
	}
}

func TestExplicitConfigurationAndInvalidExecutableFailBeforeLaunch(t *testing.T) {
	for _, name := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT"} {
		t.Run(name, func(t *testing.T) {
			audit := fixture(t, "")
			t.Setenv(name, "keep user configuration")
			if _, err := Start(context.Background()); !errors.Is(err, ErrRestricted) {
				t.Fatal("explicit configuration was overwritten or accepted")
			}
			if _, err := os.Stat(audit); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("explicit override launched native code")
			}
		})
	}
	fixture(t, "")
	t.Setenv("PATH", "")
	if _, err := Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing installation accepted")
	}
}

func TestNativeModelAndSessionMismatchNeverFallback(t *testing.T) {
	for _, mode := range []string{"model-missing", "model-unavailable", "model-overflow", "model-trailing", "session-model", "session-location", "session-permissions", "session-id", "get-mismatch", "permission-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			audit := fixture(t, mode)
			s, err := Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.CreateSession(context.Background(), t.TempDir())
			if err == nil || got != nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("unresolved/mismatched native session accepted")
			}
			creates := 0
			for _, entry := range readAudit(t, audit) {
				if entry.Path == "/api/session" {
					creates++
				}
			}
			if creates > 1 || strings.HasPrefix(mode, "model-") && creates != 0 {
				t.Fatal("model failure silently retried or created a replacement")
			}
		})
	}
}

func TestSafetyFeedFailureStopsOwnedService(t *testing.T) {
	for _, mode := range []string{"approval", "stream-loss", "event-overflow"} {
		t.Run(mode, func(t *testing.T) {
			fixture(t, mode)
			s, err := Start(context.Background())
			if err != nil {
				if mode == "approval" {
					t.Fatal(err)
				}
				return
			}
			defer s.Close()
			if mode == "approval" {
				_, _ = s.CreateSession(context.Background(), t.TempDir())
			}
			select {
			case <-s.ctx.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("unattended safety failure did not stop execution")
			}
			if mode == "approval" && !errors.Is(s.Err(), ErrRestricted) {
				t.Fatal("approval failure was not recorded as restricted")
			}
			s.Close()
		})
	}
}

func TestServiceCancellationLeavesIndependentServerAlone(t *testing.T) {
	fixture(t, "")
	independent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "independent") }))
	defer independent.Close()
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	r, err := independent.Client().Get(independent.URL)
	if err != nil {
		t.Fatal("owned cleanup stopped an unrelated service")
	}
	r.Body.Close()
}

func TestSessionKeepsEffectiveModelAndRejectsForeignBindings(t *testing.T) {
	audit := fixture(t, "default-changes")
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	directory := t.TempDir()
	session, err := s.CreateSession(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	var changed struct {
		Data Model `json:"data"`
	}
	if err := s.call(context.Background(), "GET", "/api/model/default", nil, &changed); err != nil || changed.Data.ID != "changed-default" {
		t.Fatal("fixture default did not change")
	}
	if err := s.ValidateSession(context.Background(), session); err != nil || session.Model().ID != "chat" {
		t.Fatal("changed default rebound the effective model")
	}
	before := len(readAudit(t, audit))
	for _, directory := range []string{"relative", filepath.Join(directory, "missing")} {
		if _, err := s.CreateSession(context.Background(), directory); !errors.Is(err, ErrRestricted) {
			t.Fatal("unverified directory shape accepted")
		}
	}
	if err := s.ValidateSession(context.Background(), nil); !errors.Is(err, ErrRestricted) {
		t.Fatal("missing binding accepted")
	}
	foreign := &Session{service: &Service{}, info: session.info}
	if err := s.ValidateSession(context.Background(), foreign); !errors.Is(err, ErrRestricted) {
		t.Fatal("binding from another service accepted")
	}
	if len(readAudit(t, audit)) != before {
		t.Fatal("invalid binding/directory made a native request")
	}
}

func TestNativeCreationCannotAliasAnExistingSession(t *testing.T) {
	fixture(t, "duplicate-session")
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	directory := t.TempDir()
	if _, err := s.CreateSession(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	if session, err := s.CreateSession(context.Background(), directory); !errors.Is(err, ErrRestricted) || session != nil {
		t.Fatal("native create aliased an existing session")
	}
}

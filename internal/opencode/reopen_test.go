package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// persistedNativeSession creates a real disposable native fixture with one terminal input.
func persistedNativeSession(t *testing.T) (BindingRecord, string) {
	t.Helper()
	audit := fixture(t, "")
	t.Setenv("SHELDON_NATIVE_STATE", filepath.Join(t.TempDir(), "native-state"))
	t.Setenv("SHELDON_NATIVE_CONTROL", filepath.Join(t.TempDir(), "control"))
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	session, err := s.CreateSession(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := s.Execute(context.Background(), session, "original saved input", make(chan struct{}), func(string) {})
	if !result.Idle || result.State != "completed" {
		t.Fatalf("fixture did not finish: %+v", result)
	}
	record := session.Binding()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return record, audit
}

func TestReopenExactNativeSessionWithoutDefaultsCreationOrReplay(t *testing.T) {
	record, audit := persistedNativeSession(t)
	before := len(readAudit(t, audit))
	t.Setenv("SHELDON_NATIVE_MODE", "model-unavailable")
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	session, err := s.ReopenSession(context.Background(), record, []string{"original saved input"})
	if err != nil || session.Binding() != record {
		t.Fatalf("exact native reopen: %v", err)
	}
	for _, entry := range readAudit(t, audit)[before:] {
		if entry.Path == "/api/model/default" || entry.Path == "/api/session" || strings.HasSuffix(entry.Path, "/prompt") {
			t.Fatal("reopen resolved defaults, created or replayed input")
		}
	}
	result := s.Execute(context.Background(), session, "new explicit follow-up", make(chan struct{}), func(string) {})
	if !result.Idle || result.State != "completed" {
		t.Fatalf("exact-session continuation: %+v", result)
	}
	prompts := 0
	for _, entry := range readAudit(t, audit)[before:] {
		if strings.HasSuffix(entry.Path, "/prompt") {
			prompts++
			if entry.Path != "/api/session/"+record.SessionID+"/prompt" || !strings.Contains(entry.Body, "new explicit follow-up") {
				t.Fatal("continuation changed session/input")
			}
		}
	}
	if prompts != 1 {
		t.Fatal("explicit continuation did not send exactly one new prompt")
	}
}

func TestReopenFailsClosedOnMissingIncompatibleOrUncertainNativeState(t *testing.T) {
	for _, control := range []string{"missing-session", "changed-session", "changed-location", "changed-permissions", "permission-mismatch", "history-empty", "history-truncated", "history-busy", "history-foreign", "inbox-busy"} {
		t.Run(control, func(t *testing.T) {
			record, audit := persistedNativeSession(t)
			if err := os.WriteFile(os.Getenv("SHELDON_NATIVE_CONTROL"), []byte(control), 0600); err != nil {
				t.Fatal(err)
			}
			before := len(readAudit(t, audit))
			s, err := Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if session, err := s.ReopenSession(context.Background(), record, []string{"original saved input"}); err == nil || session != nil {
				t.Fatal("unsafe reopen returned a usable session")
			}
			for _, entry := range readAudit(t, audit)[before:] {
				if entry.Path == "/api/model/default" || entry.Path == "/api/session" || strings.HasSuffix(entry.Path, "/prompt") || strings.HasSuffix(entry.Path, "/interrupt") {
					t.Fatal("failed reopen mutated native state or dispatched input")
				}
			}
		})
	}
}

func TestReopenRejectsIncompatibleSavedPolicyBeforeNativeLookup(t *testing.T) {
	record, audit := persistedNativeSession(t)
	s, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []string{"version", "protocol", "policy", "location", "model"} {
		broken := record
		switch kind {
		case "version":
			broken.Version = "2.2.0"
		case "protocol":
			broken.Protocol = "other"
		case "policy":
			broken.Policy++
		case "location":
			broken.Directory = "relative"
		case "model":
			broken.Model.ID = ""
		}
		before := len(readAudit(t, audit))
		if session, err := s.ReopenSession(context.Background(), broken, []string{"original saved input"}); err == nil || session != nil {
			t.Fatal("incompatible saved binding accepted")
		}
		if len(readAudit(t, audit)) != before {
			t.Fatal("invalid binding contacted native service")
		}
	}
}

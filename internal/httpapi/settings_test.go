package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/settings"
)

const confirmationBody = `{"schemaVersion":1,"confirm":true,"defaultHarness":"opencode","models":{"opencode":{"mode":"default"}}}`
const testAnswerBody = `{"clientRequestID":"fixture-request","harness":"opencode","selection":{"mode":"default"},"confirmQuota":true}`

func settingsCanaries(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PATH", root)
	marker := filepath.Join(root, "native-command-ran")
	t.Setenv("SHELDON_SETTINGS_TEST_MARKER", marker)
	if runtime.GOOS != "windows" {
		for _, name := range []string{"git", "gh", "opencode", "codex", "claude"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/sh\n: > \"$SHELDON_SETTINGS_TEST_MARKER\"\nexit 91\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	return marker
}

func TestSettingsContractsNeverScanSaveOrAdmitInference(t *testing.T) {
	marker := settingsCanaries(t)
	s, client := newTestServer(t, "")
	auth := map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"}
	r := request(t, s, client, "GET", "/api/settings", "", auth)
	var state settings.State
	if r.StatusCode != 200 || json.NewDecoder(r.Body).Decode(&state) != nil || state.Preferences.SetupConfirmed || state.Preferences.DefaultHarness != "" || state.Persistence.Status != "unsupported" {
		t.Fatal("settings pretended to be persisted/confirmed")
	}
	for _, id := range []string{"opencode", "codex", "claude"} {
		r = request(t, s, client, "GET", "/api/environment/harnesses/"+id+"/models", "", auth)
		var models settings.ModelDiscovery
		if r.StatusCode != 200 || json.NewDecoder(r.Body).Decode(&models) != nil || models.Catalog.Status != "unknown" || models.DefaultModel != nil || len(models.Models) != 0 || models.ExplicitSelection.Status != "unsupported" {
			t.Fatal("invented native model metadata")
		}
	}
	for _, input := range []struct{ method, path, body string }{{"PUT", "/api/settings", confirmationBody}, {"POST", "/api/environment/test-runs", testAnswerBody}} {
		r = request(t, s, client, input.method, input.path, input.body, auth)
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != http.StatusNotImplemented || strings.Contains(string(body), `"id"`) {
			t.Fatal("unimplemented save/test returned simulated success or admission")
		}
	}
	r = request(t, s, client, "GET", "/api/settings", "", auth)
	if json.NewDecoder(r.Body).Decode(&state) != nil || state.Preferences.SetupConfirmed || state.Preferences.UpdatedAt != nil {
		t.Fatal("failed confirmation changed preferences")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("settings/model/test endpoints invoked a native executable")
	}
}

func TestSettingsRoutesRetainBearerHostAndOriginProtection(t *testing.T) {
	settingsCanaries(t)
	s, client := newTestServer(t, "")
	for _, input := range []struct{ method, path, body string }{
		{"GET", "/api/settings", ""}, {"PUT", "/api/settings", confirmationBody},
		{"GET", "/api/environment/harnesses/opencode/models", ""}, {"POST", "/api/environment/test-runs", testAnswerBody},
	} {
		for _, headers := range []map[string]string{nil, {"Cookie": "sheldon_session=" + s.APIToken()}, {"Authorization": "Bearer invalid"}} {
			if request(t, s, client, input.method, input.path, input.body, headers).StatusCode != 401 {
				t.Fatal("settings route exposed without bearer")
			}
		}
		for _, headers := range []map[string]string{
			{"Authorization": "Bearer " + s.APIToken(), "Origin": "https://attacker.example"},
			{"Authorization": "Bearer " + s.APIToken(), "Host": "attacker.example"},
		} {
			if request(t, s, client, input.method, input.path, input.body, headers).StatusCode != 403 {
				t.Fatal("settings route bypassed authority/origin guard")
			}
		}
	}
}

func TestSettingsRequestsAreBoundedStrictAndNeverEchoSecrets(t *testing.T) {
	marker := settingsCanaries(t)
	s, client := newTestServer(t, "")
	auth := map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"}
	for _, input := range []struct {
		method, path, body string
		status             int
	}{
		{"PUT", "/api/settings", strings.Replace(confirmationBody, `"confirm":true`, `"confirm":false`, 1), 400},
		{"PUT", "/api/settings", strings.Replace(confirmationBody, `"schemaVersion":1`, `"schemaVersion":99`, 1), 400},
		{"PUT", "/api/settings", `{"apiKey":"private-submitted-secret"}`, 400},
		{"PUT", "/api/settings", strings.TrimSuffix(confirmationBody, "}") + `,"updatedAt":"2026-10-28T00:00:00Z"}`, 400},
		{"PUT", "/api/settings", confirmationBody + ` {}`, 400},
		{"PUT", "/api/settings", `{"value":"` + strings.Repeat("x", 8192) + `"}`, 413},
		{"POST", "/api/environment/test-runs", strings.Replace(testAnswerBody, `"confirmQuota":true`, `"confirmQuota":false`, 1), 400},
		{"POST", "/api/environment/test-runs", strings.TrimSuffix(testAnswerBody, "}") + `,"prompt":"private-submitted-secret"}`, 400},
		{"POST", "/api/environment/test-runs", strings.TrimSuffix(testAnswerBody, "}") + `,"nativeSessionID":"private-submitted-secret"}`, 400},
		{"GET", "/api/environment/harnesses/unknown/models", "", 404},
		{"GET", "/api/environment/harnesses/opencode/models?target=http://example.invalid", "", 400},
	} {
		r := request(t, s, client, input.method, input.path, input.body, auth)
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != input.status || strings.Contains(string(body), "private-submitted-secret") || strings.Contains(string(body), s.APIToken()) {
			t.Fatalf("%s %s: unsafe status %d (want %d)", input.method, input.path, r.StatusCode, input.status)
		}
	}
	if request(t, s, client, "PUT", "/api/settings", confirmationBody, map[string]string{"Authorization": auth["Authorization"], "Content-Type": "text/plain"}).StatusCode != 415 {
		t.Fatal("non-JSON settings accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("invalid input invoked a native command")
	}
}

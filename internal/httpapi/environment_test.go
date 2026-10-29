package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/environment"
)

func TestEnvironmentIsProtectedAndReportsMissingWithoutNativeCalls(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s, client := newTestServer(t, "")
	for _, headers := range []map[string]string{
		nil,
		{"Cookie": "sheldon_session=" + s.APIToken()},
		{"Authorization": "Bearer invalid"},
	} {
		r := request(t, s, client, "GET", "/api/environment", "", headers)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatal("environment metadata exposed without bearer")
		}
	}
	auth := map[string]string{"Authorization": "Bearer " + s.APIToken()}
	r := request(t, s, client, "GET", "/api/environment", "", auth)
	var report environment.Report
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&report) != nil || len(report.Tools) != 5 {
		t.Fatal("missing environment report")
	}
	for _, tool := range report.Tools {
		if tool.Status != environment.Missing {
			t.Fatalf("unexpected tool: %+v", tool)
		}
	}
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("environment report was cacheable")
	}
	for _, headers := range []map[string]string{
		{"Authorization": auth["Authorization"], "Origin": "https://attacker.example"},
		{"Authorization": auth["Authorization"], "Host": "attacker.example"},
	} {
		if request(t, s, client, "GET", "/api/environment", "", headers).StatusCode != http.StatusForbidden {
			t.Fatal("environment bypassed authority/origin checks")
		}
	}
	if request(t, s, client, "GET", "/api/environment?command=auth", "", auth).StatusCode != http.StatusBadRequest {
		t.Fatal("environment accepted command parameters")
	}
}

func TestEnvironmentBoundsScansAndHeadDoesNotScan(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s, client := newTestServer(t, "")
	auth := map[string]string{"Authorization": "Bearer " + s.APIToken()}
	s.environmentBusy <- struct{}{}
	r := request(t, s, client, "GET", "/api/environment", "", auth)
	if r.StatusCode != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
		t.Fatal("simultaneous scans were not bounded")
	}
	r = request(t, s, client, "HEAD", "/api/environment", "", auth)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK || len(body) != 0 {
		t.Fatal("HEAD should not start metadata processes")
	}
	<-s.environmentBusy
	r = request(t, s, client, "GET", "/api/environment", "", auth)
	if r.StatusCode != http.StatusOK || len(s.environmentBusy) != 0 {
		t.Fatal("scan did not release its slot")
	}
}

func TestEnvironmentFailureWithholdsPrivatePaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "private-temp-root-not-created")
	s, client := newTestServer(t, "")
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, missing)
	}
	r := request(t, s, client, "GET", "/api/environment", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusServiceUnavailable || strings.Contains(string(body), missing) || strings.Contains(string(body), s.APIToken()) {
		t.Fatal("environment error lacked a safe public diagnostic")
	}
	if len(s.environmentBusy) != 0 {
		t.Fatal("failed scan kept its slot")
	}
}

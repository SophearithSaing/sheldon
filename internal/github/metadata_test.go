package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

var fixtureBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sheldon-gh-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtureBin = dir
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./testdata/gh")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fixture: %v %s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// metadataFixture selects the synthetic CLI and disposable request storage.
func metadataFixture(t *testing.T) (githubpr.PullRequest, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PATH", fixtureBin)
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	t.Setenv("GH_TOKEN", "synthetic-auth-token")
	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GH_HOST", "attacker.invalid")
	t.Setenv("GH_PAGER", "untrusted-pager")
	t.Setenv("PAGER", "untrusted-pager")
	t.Setenv("GH_FORCE_TTY", "120")
	t.Setenv("SHELDON_TEST_GH_MODE", "")
	audit := filepath.Join(root, "audit.json")
	t.Setenv("SHELDON_TEST_GH_AUDIT", audit)
	body, err := os.ReadFile("testdata/pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELDON_TEST_GH_RESPONSE", "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n"+string(body))
	pr, err := githubpr.ParseURL("https://github.com/OWNER/REPO/pull/31")
	if err != nil {
		t.Fatal(err)
	}
	return pr, audit
}

func TestResolveNativeMetadata(t *testing.T) {
	pr, auditPath := metadataFixture(t)
	config := filepath.Join(t.TempDir(), "config.yml")
	before := []byte("synthetic configured authentication\n")
	if err := os.WriteFile(config, before, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_CONFIG_DIR", filepath.Dir(config))
	r, err := Resolve(context.Background(), pr)
	if err != nil || r.State != Resolved || r.Base == nil || r.Base.Branch != "feat/go-rewrite" || r.Base.Source != githubpr.GitHubBase || r.Base.SHA != strings.Repeat("a", 40) || r.HeadSHA != strings.Repeat("b", 40) || r.Failure != nil {
		t.Fatalf("metadata resolution: %+v, %v", r, err)
	}
	if r.PR.ID != pr.ID || r.PR.Title == nil || *r.PR.Title != "Explain changed refresh behavior" || r.PR.Author == nil || *r.PR.Author != "example-author" {
		t.Fatal("metadata lost identity/display fields")
	}
	if _, err := r.ChooseGitOnly(); err == nil {
		t.Fatal("resolved metadata silently changed to Git-only")
	}
	data, err := os.ReadFile(auditPath)
	var audit struct {
		Directory                                                              string
		Arguments, StdinClosed, AuthenticationPreserved, RestrictedEnvironment bool
	}
	if err != nil || json.Unmarshal(data, &audit) != nil || !audit.Arguments || !audit.StdinClosed || !audit.AuthenticationPreserved || !audit.RestrictedEnvironment {
		t.Fatal("native command contract failed")
	}
	if !filepath.IsAbs(audit.Directory) || filepath.Dir(audit.Directory) != filepath.Dir(auditPath) {
		t.Fatal("request did not use private OS temporary storage")
	}
	if _, err := os.Stat(audit.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("request directory was not cleaned up")
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(before, after) || os.Getenv("GH_HOST") != "attacker.invalid" {
		t.Fatal("resolver changed parent configuration/environment")
	}
}

func TestMetadataFailuresRequireExplicitFallback(t *testing.T) {
	valid, err := os.ReadFile("testdata/pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, mode, response, code string
		status                     int
	}{
		{"auth", "auth", "", "authentication-required", 0},
		{"http auth", "failed", "HTTP/1.1 401 Unauthorized\n\n{}", "authentication-required", 401},
		{"restricted", "failed", "HTTP/2.0 403 Forbidden\r\n\r\n{}", "access-or-usage-restricted", 403},
		{"rate limit", "failed", "HTTP/2.0 429 Too Many Requests\r\n\r\n{}", "access-or-usage-restricted", 429},
		{"missing or private", "failed", "HTTP/2.0 404 Not Found\r\n\r\n{}", "not-found-or-inaccessible", 404},
		{"service", "failed", "HTTP/2.0 503 Unavailable\r\n\r\n{}", "service-unavailable", 503},
		{"unknown", "misleading-stderr", "", "cli-failed", 0},
		{"failed valid output", "failed", "HTTP/2.0 200 OK\r\n\r\n" + string(valid), "cli-failed", 200},
		{"stdout limit", "large-stdout", "", "output-limit", 0},
		{"stderr limit", "large-stderr", "", "output-limit", 0},
		{"bad framing", "", "{not-native-headers}", "invalid-response", 0},
		{"bad metadata", "", "HTTP/2.0 200 OK\r\n\r\n{}", "invalid-metadata", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pr, _ := metadataFixture(t)
			t.Setenv("SHELDON_TEST_GH_MODE", tt.mode)
			t.Setenv("SHELDON_TEST_GH_RESPONSE", tt.response)
			r, err := Resolve(context.Background(), pr)
			if err != nil || r.State != FallbackRequired || r.Base != nil || r.HeadSHA != "" || r.Failure == nil || r.Failure.Code != tt.code || r.Failure.HTTPStatus != tt.status {
				t.Fatalf("fallback: %+v, %v", r, err)
			}
			if tt.code == "output-limit" && !errors.Is(r.Failure, process.ErrOutputLimit) {
				t.Fatal("output-limit cause lost")
			}
			data, _ := json.Marshal(r.Failure)
			for _, text := range []string{r.Failure.Error(), fmt.Sprintf("%+v", r.Failure), fmt.Sprintf("%#v", r.Failure), string(data)} {
				if strings.Contains(text, "synthetic-auth-token") || strings.Contains(text, "private-diagnostic") || strings.Contains(text, "secret@example") {
					t.Fatal("failure exposed native output")
				}
			}
			manual, err := r.ChooseGitOnly()
			if err != nil || manual.State != BaseSelectionRequired || manual.Base != nil || manual.HeadSHA != "" || manual.Failure != r.Failure || r.State != FallbackRequired {
				t.Fatal("fallback consent invented a base or mutated prior state")
			}
		})
	}
}

func TestMetadataPrerequisitesAndCancellation(t *testing.T) {
	pr, audit := metadataFixture(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := Resolve(cancelled, pr); !errors.Is(err, context.Canceled) || r.State != "" {
		t.Fatal("cancelled request offered fallback")
	}
	invalid := pr
	invalid.ID = "../escape"
	if r, err := Resolve(context.Background(), invalid); err == nil || r.State != "" {
		t.Fatal("invalid input produced a resolution")
	}
	if _, err := os.Stat(audit); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid/cancelled input invoked gh")
	}
	t.Setenv("SHELDON_TEST_GH_MODE", "hang")
	ctx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	if r, err := Resolve(ctx, pr); !errors.Is(err, context.DeadlineExceeded) || r.State != "" {
		t.Fatal("expired request produced fallback/success")
	}
	t.Setenv("PATH", "")
	r, err := Resolve(context.Background(), pr)
	if err != nil || r.Failure == nil || r.Failure.Code != "cli-missing" || r.Base != nil {
		t.Fatalf("missing gh: %+v %v", r, err)
	}
	t.Setenv("PATH", fixtureBin)
	t.Setenv("TMPDIR", "relative")
	t.Setenv("TMP", "relative")
	t.Setenv("TEMP", "relative")
	r, err = Resolve(context.Background(), pr)
	if err != nil || r.Failure == nil || r.Failure.Code != "temporary-storage" {
		t.Fatal("relative temporary root accepted")
	}
}

func TestParseMetadataIdentityAndOptionalDescriptions(t *testing.T) {
	pr, _ := metadataFixture(t)
	body, _ := os.ReadFile("testdata/pull-request.json")
	var original map[string]any
	if err := json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"number": func(v map[string]any) { v["number"] = 32 },
		"URL":    func(v map[string]any) { v["html_url"] = "https://github.com/owner/other/pull/31" },
		"repository": func(v map[string]any) {
			v["base"].(map[string]any)["repo"].(map[string]any)["full_name"] = "other/repo"
		},
		"base branch":      func(v map[string]any) { v["base"].(map[string]any)["ref"] = "" },
		"unsafe branch":    func(v map[string]any) { v["base"].(map[string]any)["ref"] = "--help" },
		"base commit":      func(v map[string]any) { v["base"].(map[string]any)["sha"] = strings.Repeat("0", 40) },
		"head commit":      func(v map[string]any) { v["head"].(map[string]any)["sha"] = "b123456" },
		"mixed formats":    func(v map[string]any) { v["head"].(map[string]any)["sha"] = strings.Repeat("b", 64) },
		"uppercase commit": func(v map[string]any) { v["head"].(map[string]any)["sha"] = strings.Repeat("B", 40) },
		"title type":       func(v map[string]any) { v["title"] = 5 },
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			json.Unmarshal(body, &changed)
			mutate(changed)
			data, _ := json.Marshal(changed)
			if _, err := parseMetadata(data, pr); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	delete(original, "title")
	original["user"] = nil
	data, _ := json.Marshal(original)
	r, err := parseMetadata(data, pr)
	if err != nil || r.PR.Title != nil || r.PR.Author != nil || r.Base.Branch != "feat/go-rewrite" {
		t.Fatal("unknown descriptions manufactured values or lost valid commits")
	}
	for _, malformed := range [][]byte{append(bytes.Clone(body), []byte(" {}")...), []byte("\xff"), []byte(`{"number":"31"}`)} {
		if _, err := parseMetadata(malformed, pr); err == nil {
			t.Fatal("malformed metadata accepted")
		}
	}
}

func TestNativeIncludeFraming(t *testing.T) {
	for _, headers := range []string{
		"HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n",
		"HTTP/1.1 200 OK\nContent-Type: application/json\r\n\r\n",
		"HTTP/1.1 200 OK\nTransfer-Encoding: chunked\n\n",
	} {
		status, body, err := responseBody([]byte(headers + "{}"))
		if err != nil || status != 200 || string(body) != "{}" {
			t.Fatalf("native include framing: %d %s %v", status, body, err)
		}
	}
	for _, data := range []string{"{}", "HTTP/2.0 200 OK\nmalformed\n\n{}", "HTTP/2.0 abc\n\n{}", "HTTP/2.0 200 OK\nX-Long: " + strings.Repeat("a", 16<<10) + "\n\n{}"} {
		if _, _, err := responseBody([]byte(data)); err == nil {
			t.Fatal("invalid header framing accepted")
		}
	}
}

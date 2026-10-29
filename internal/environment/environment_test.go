package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fixtureBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sheldon-environment-tests-")
	if err != nil {
		panic(err)
	}
	fixtureBinary = filepath.Join(dir, "tool")
	if runtime.GOOS == "windows" {
		fixtureBinary += ".exe"
	}
	command := exec.Command("go", "build", "-o", fixtureBinary, "./testdata/tool")
	if output, err := command.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "build environment fixture: %v\n%s", err, output)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func fixtureEnvironment(t *testing.T, installed ...string) string {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "tools 日本語 with spaces")
	log := filepath.Join(root, "calls")
	for _, dir := range []string{bin, log} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range installed {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		destination := filepath.Join(bin, name)
		if err := os.Link(fixtureBinary, destination); err != nil {
			source, err := os.Open(fixtureBinary)
			if err != nil {
				t.Fatal(err)
			}
			target, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY, 0700)
			if err != nil {
				source.Close()
				t.Fatal(err)
			}
			_, err = io.Copy(target, source)
			source.Close()
			target.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("SHELDON_ENV_TEST_LOG", log)
	for _, spec := range tools {
		t.Setenv("SHELDON_ENV_TEST_"+strings.ToUpper(spec.id), "")
	}
	return log
}

func inspect(t *testing.T) Report {
	t.Helper()
	report, err := Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestDetectedVersionsAreNotReadinessOrEntitlement(t *testing.T) {
	log := fixtureEnvironment(t, "git", "gh", "opencode", "codex", "claude")
	report := inspect(t)
	if len(report.Tools) != 5 {
		t.Fatal("missing diagnostics")
	}
	for _, tool := range report.Tools {
		if tool.Status != Available || tool.Version == "" {
			t.Fatalf("%s: %+v", tool.ID, tool)
		}
		if tool.Optional != (tool.ID == "gh") {
			t.Fatal("only gh should be optional")
		}
		if c := tool.Capabilities; c != nil {
			if c.Protocol.Status != Available || c.Service.Status != Unknown || c.Policy.Status != Unknown || c.Authentication.Status != Unknown || c.Entitlement.Status != Unknown || c.Compatibility.Status != Unknown {
				t.Fatalf("installation was mistaken for readiness: %+v", c)
			}
		}
		data, err := os.ReadFile(filepath.Join(log, tool.ID+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		want := 1
		if tool.Capabilities != nil {
			want = 2
		}
		if tool.ID == "codex" {
			want = 3
		}
		if len(lines) != want {
			t.Fatalf("unexpected invocations for %s: %s", tool.ID, data)
		}
		if (tool.ID == "codex" || tool.ID == "claude") && !strings.Contains(tool.Capabilities.Policy.Detail, "advertises permission/approval") {
			t.Fatal("available policy metadata was not reported")
		}
		for _, line := range lines {
			var call struct {
				Args  []string
				Dir   string
				Stdin int
			}
			if err := json.Unmarshal([]byte(line), &call); err != nil {
				t.Fatal(err)
			}
			if call.Stdin != 0 || !strings.HasPrefix(filepath.Base(call.Dir), "sheldon-environment-") {
				t.Fatal("probe had input or repository cwd")
			}
			if _, err := os.Stat(call.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("probe working directory not cleaned up")
			}
		}
	}
	data, _ := json.Marshal(report)
	for _, secret := range []string{"private-provider-secret", log, fixtureBinary} {
		if strings.Contains(string(data), secret) {
			t.Fatal("raw diagnostic or private path exposed")
		}
	}
}

func TestMissingAndUnsupportedAreDistinctFromUnknown(t *testing.T) {
	fixtureEnvironment(t)
	for _, tool := range inspect(t).Tools {
		if tool.Status != Missing || tool.Version != "" {
			t.Fatalf("missing: %+v", tool)
		}
	}
	fixtureEnvironment(t, "opencode")
	for _, mode := range []string{"legacy", "future", "prerelease"} {
		t.Setenv("SHELDON_ENV_TEST_OPENCODE", mode)
		tool := inspect(t).Tools[2]
		want := Unknown
		if mode == "legacy" {
			want = Unsupported
		}
		if tool.Capabilities.Compatibility.Status != want || tool.Capabilities.Protocol.Status != want {
			t.Fatalf("%s: %+v", mode, tool)
		}
	}
}

func TestProbeFailuresAndHiddenFlagsDoNotBecomeUnsupported(t *testing.T) {
	fixtureEnvironment(t, "opencode")
	for _, mode := range []string{"fail", "bad-version", "overflow", "stderr-overflow", "hang", "fail-help", "hang-help", "hidden-help"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SHELDON_ENV_TEST_OPENCODE", mode)
			tool := inspect(t).Tools[2]
			if tool.Capabilities.Protocol.Status != Unknown || tool.Capabilities.Compatibility.Status != Unknown {
				t.Fatalf("%s: %+v", mode, tool)
			}
			if strings.Contains(fmt.Sprint(tool), "private-provider-secret") {
				t.Fatal("probe failure leaked output")
			}
			if !strings.Contains(mode, "help") && tool.Status != Unknown {
				t.Fatal("failed version check claimed a known version")
			}
		})
	}
}

func TestServiceFixturesAreNeverContactedOrTreatedAsReady(t *testing.T) {
	fixtureEnvironment(t, "opencode")
	for _, status := range []int{200, 401, 404, 503} {
		var requests atomic.Int32
		service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(status) }))
		t.Setenv("SHELDON_ENV_TEST_SERVICE", service.URL)
		t.Setenv("OPENCODE_SERVER", service.URL)
		report := inspect(t)
		service.Close()
		if requests.Load() != 0 || report.Tools[2].Capabilities.Service.Status != Unknown {
			t.Fatal("service state was inferred or service was contacted")
		}
	}
}

func TestCancellationAndTempFailuresWithholdPaths(t *testing.T) {
	fixtureEnvironment(t, "opencode")
	t.Setenv("SHELDON_ENV_TEST_OPENCODE", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Inspect(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancelled probe did not stop promptly")
	}
	missing := filepath.Join(t.TempDir(), "private-missing-temp")
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, missing)
	}
	if _, err := Inspect(context.Background()); err == nil || strings.Contains(err.Error(), missing) {
		t.Fatal("temporary-directory error missing or exposed path")
	}
}

func TestProviderEnvironmentAndPrivateConfigurationRemainUntouched(t *testing.T) {
	fixtureEnvironment(t, "opencode", "codex", "claude")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENAI_BASE_URL", "https://synthetic-provider.invalid/v1")
	t.Setenv("SHELDON_ENV_TEST_EXPECT_ROUTE", "https://synthetic-provider.invalid/v1")
	t.Setenv("OPENAI_API_KEY", "synthetic-provider-secret-do-not-display")
	files := []string{"opencode.json", ".claude.json", "provider-auth-fixture.json"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(home, name), []byte("private synthetic configuration"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	report := inspect(t)
	for _, tool := range report.Tools[2:] {
		if tool.Status != Available {
			t.Fatal("normal provider environment was not preserved")
		}
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(data) != "private synthetic configuration" {
			t.Fatal("user configuration was modified")
		}
	}
	data, _ := json.Marshal(report)
	for _, private := range []string{home, "synthetic-provider", "private synthetic configuration"} {
		if strings.Contains(string(data), private) {
			t.Fatal("private state exposed")
		}
	}
}

func TestRelativePATHAndShellShimsAreNotExecuted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows shim resolution requires native validation")
	}
	log := fixtureEnvironment(t)
	bin := os.Getenv("PATH")
	shim := filepath.Join(bin, "shim.cmd")
	if err := os.Link(fixtureBinary, shim); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shim, filepath.Join(bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	if tool := inspect(t).Tools[2]; tool.Status != Unsupported {
		t.Fatalf("shell shim: %+v", tool)
	}
	if entries, err := os.ReadDir(log); err != nil || len(entries) != 0 {
		t.Fatal("shell shim executed")
	}
	parent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(parent, bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", relative)
	if tool := inspect(t).Tools[2]; tool.Status != Unknown {
		t.Fatalf("relative PATH: %+v", tool)
	}
}

//go:build linux || darwin

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// appEnvironment exercises real path resolution and desktop-command invocation.
// The executable fixture records literal arguments only in private test storage.
type appEnvironment struct {
	paths platform.Paths
	log   string
}

func newAppEnvironment(t *testing.T) appEnvironment {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "browser-arguments")
	// Quoted positional parameters keep the URL as data, not shell syntax.
	script := `#!/bin/sh
umask 077
if [ "$#" -ne 1 ]; then exit 2; fi
printf '%s\n' "$1" >> "$SHELDON_TEST_BROWSER_LOG"
if [ "$SHELDON_TEST_BROWSER_FAIL" = 1 ]; then
  printf 'fake opener error: %s\n' "$1" >&2
  exit 1
fi
`
	for _, name := range []string{"xdg-open", "open"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("PATH", bin)
	t.Setenv("SHELDON_TEST_BROWSER_LOG", log)
	t.Setenv("SHELDON_TEST_BROWSER_FAIL", "")
	t.Setenv("SHELDON_DEV_ORIGIN", "")
	t.Setenv("SHELDON_DEV_LISTEN", "")
	paths, err := platform.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return appEnvironment{paths: paths, log: log}
}

func (e appEnvironment) launches(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(e.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	return lines[:len(lines)-1] // Ignore an incomplete last write.
}

func (e appEnvironment) waitLaunches(t *testing.T, count int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if targets := e.launches(t); len(targets) >= count {
			return targets
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("desktop opener was not invoked")
	return nil
}

type runningApp struct {
	target string
	cancel context.CancelFunc
	done   chan error
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func startApp(t *testing.T, env appEnvironment, prURL string) *runningApp {
	t.Helper()
	count := len(env.launches(t))
	a := &runningApp{done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go func() {
		a.done <- Run(ctx, prURL, &a.stdout, &a.stderr)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-a.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("runtime did not stop")
		}
	})
	a.target = env.waitLaunches(t, count+1)[count]
	return a
}

func readOpenState(t *testing.T, target string) (string, string) {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	token := fragment.Get("token")
	u.Fragment = ""
	u.Path = "/api/open"
	r, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	c := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		URL string `json:"url"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&body) != nil {
		t.Fatal("cannot read pending open intent")
	}
	return body.URL, token
}

func TestFirstLaunchAndSecondInstanceForwarding(t *testing.T) {
	env := newAppEnvironment(t)
	a := startApp(t, env, "https://github.com/owner/repo/pull/1")
	state, token := readOpenState(t, a.target)
	if state != "https://github.com/owner/repo/pull/1" {
		t.Fatal("initial PR request was lost")
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Run(ctx, "https://github.com/owner/repo/pull/2#discussion", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if targets := env.launches(t); len(targets) != 2 || targets[1] != a.target {
		t.Fatal("second process started another server or changed its credential")
	}
	state, _ = readOpenState(t, a.target)
	if state != "https://github.com/owner/repo/pull/2" {
		t.Fatal("second-instance request not delivered")
	}
	if err := Run(ctx, "", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	state, _ = readOpenState(t, a.target)
	if state != "https://github.com/owner/repo/pull/2" {
		t.Fatal("plain launch cleared pending PR")
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatal("CLI output exposed token")
	}
	// A changed runtime/cache override must not create a second data-root writer.
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "another-runtime"))
	if err := Run(ctx, "", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if targets := env.launches(t); len(targets) != 4 || targets[3] != a.target {
		t.Fatal("data-root locking was bypassed")
	}
}

func TestDevelopmentLaunchAndForwardingUseTheOwnersVerifiedUI(t *testing.T) {
	env := newAppEnvironment(t)
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer reserved.Close()
	target, err := url.Parse("http://" + reserved.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		origin := "http://" + r.Host
		director(r)
		r.Host = target.Host
		r.Header.Set("Origin", origin)
	}
	frontend := httptest.NewServer(proxy)
	defer frontend.Close()
	t.Setenv("SHELDON_DEV_ORIGIN", frontend.URL)
	t.Setenv("SHELDON_DEV_LISTEN", target.Host)
	reserved.Close()
	a := startApp(t, env, "https://github.com/owner/repo/pull/1")
	if !strings.HasPrefix(a.target, frontend.URL+"/#token=") {
		t.Fatal("development launch did not use the verified proxy")
	}
	// A plain later invocation uses the owner's recorded UI, not its own environment.
	t.Setenv("SHELDON_DEV_ORIGIN", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Run(ctx, "https://github.com/owner/repo/pull/2", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if launches := env.launches(t); len(launches) != 2 || launches[1] != a.target {
		t.Fatal("forwarder lost the owner's development UI")
	}
	state, _ := readOpenState(t, a.target)
	if state != "https://github.com/owner/repo/pull/2" {
		t.Fatal("development open request was not forwarded")
	}
	t.Setenv("SHELDON_DEV_ORIGIN", "http://127.0.0.1:5176")
	if Run(ctx, "https://github.com/owner/repo/pull/3", io.Discard, io.Discard) == nil {
		t.Fatal("different development configuration silently reused the owner")
	}
	state, _ = readOpenState(t, a.target)
	if state != "https://github.com/owner/repo/pull/2" {
		t.Fatal("mismatched development launch changed the pending PR")
	}
}

func TestUnverifiedDevelopmentUIKeepsBackendRunningWithoutLaunchingBrowser(t *testing.T) {
	env := newAppEnvironment(t)
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unverified UI received a bearer credential")
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer frontend.Close()
	t.Setenv("SHELDON_DEV_ORIGIN", frontend.URL)
	t.Setenv("SHELDON_DEV_LISTEN", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(4 * time.Second):
			t.Error("runtime did not stop")
		}
		reader.Close()
		writer.Close()
	})
	go func() { done <- Run(ctx, "", io.Discard, writer) }()
	guidance := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(reader).ReadString('\n'); guidance <- line }()
	select {
	case line := <-guidance:
		if !strings.Contains(line, "Development UI is unavailable or unverified") {
			t.Fatal("missing development connection guidance")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("no guidance for an unverified development UI")
	}
	i, err := openInstance(env.paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	state, err := i.read()
	if err != nil {
		t.Fatal(err)
	}
	client, err := httpapi.NewClient(state.URL, state.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Verify(ctx); err != nil {
		t.Fatal("backend stopped when the development UI was unavailable")
	}
	if len(env.launches(t)) != 0 {
		t.Fatal("opened an unverified browser origin")
	}
}

func TestShutdownClearsDiscoveryAndReleasesListener(t *testing.T) {
	env := newAppEnvironment(t)
	paths := env.paths
	a := startApp(t, env, "")
	u, _ := url.Parse(a.target)
	a.cancel()
	select {
	case err := <-a.done:
		a.done <- err
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown blocked")
	}
	conn, err := net.DialTimeout("tcp", u.Host, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("listener survived shutdown")
	}
	func() {
		i, err := openInstance(paths.Data)
		if err != nil {
			t.Fatal(err)
		}
		defer i.close()
		if owned, err := i.acquire(); err != nil || !owned {
			t.Fatalf("lock not released: %v", err)
		}
		if _, err := i.read(); err == nil {
			t.Fatal("credential remained in discovery after shutdown")
		}
	}()
	b := startApp(t, env, "")
	if b.target == a.target {
		t.Fatal("new start reused origin and token")
	}
}

func TestStaleUnlockedRecordIsReplaced(t *testing.T) {
	env := newAppEnvironment(t)
	paths := env.paths
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := i.acquire()
	if err != nil || !owned {
		t.Fatal("could not own fixture lock")
	}
	if err := i.publish(record{URL: "http://192.0.2.1:1234", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	// Simulate abrupt exit: close the OS handle without clearing the old record.
	if err := i.file.Close(); err != nil {
		t.Fatal(err)
	}
	a := startApp(t, env, "")
	_, token := readOpenState(t, a.target)
	if token == "stale" {
		t.Fatal("stale credential reused")
	}
}

func TestHeldUnavailableInstanceFailsWithoutLaunching(t *testing.T) {
	env := newAppEnvironment(t)
	paths := env.paths
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatal("could not own fixture lock")
	}
	if err := i.publish(record{URL: "http://127.0.0.1:1", Token: "invalid"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	err = Run(ctx, "", io.Discard, io.Discard)
	if err == nil {
		t.Fatal("held but unavailable instance accepted")
	}
	if _, err := i.read(); err != nil {
		t.Fatal("contender modified owner's record")
	}
	if len(env.launches(t)) != 0 {
		t.Fatal("launched an unverified server")
	}
}

func TestUnverifiedInstanceRetriesWithDelay(t *testing.T) {
	env := newAppEnvironment(t)
	i, err := openInstance(env.paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	owned, err := i.acquire()
	if err != nil || !owned {
		t.Fatal("could not own fixture lock")
	}

	var attempts atomic.Int32
	unverified := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("unverified listener received a bearer credential")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unverified.Close()
	if err := i.publish(record{URL: unverified.URL, Token: strings.Repeat("A", 43)}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 220*time.Millisecond)
	defer cancel()
	err = Run(ctx, "", io.Discard, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if count := attempts.Load(); count < 1 || count > 6 {
		t.Fatalf("expected bounded verification retries, got %d", count)
	}
	if len(env.launches(t)) != 0 {
		t.Fatal("launched an unverified server")
	}
}

func TestContenderAcquiresAfterOwnerExitsBeforePublish(t *testing.T) {
	env := newAppEnvironment(t)
	paths := env.paths
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatal("could not own fixture lock")
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		done <- Run(ctx, "", io.Discard, io.Discard)
	}()
	// Releasing an owner that never published must let the contender acquire it.
	if err := i.close(); err != nil {
		t.Fatal(err)
	}
	env.waitLaunches(t, 1)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("contender did not shut down")
	}
}

func TestContenderWaitsForPublication(t *testing.T) {
	env := newAppEnvironment(t)
	paths := env.paths
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatal("could not own fixture lock")
	}
	s, err := httpapi.Listen("", "")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Shutdown(ctx)
		<-served
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "", io.Discard, io.Discard)
	}()
	select {
	case err := <-done:
		t.Fatalf("contender exited before publication: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	if len(env.launches(t)) != 0 {
		t.Fatal("contender launched without a published record")
	}
	if err := i.publish(record{URL: s.URL(), Token: s.APIToken()}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("contender did not discover ready owner")
	}
	if targets := env.launches(t); len(targets) != 1 || !strings.HasPrefix(targets[0], s.URL()+"/#token=") {
		t.Fatal("contender used wrong owner")
	}
}

func TestBrowserFailureKeepsRuntimeAndRedactsURL(t *testing.T) {
	env := newAppEnvironment(t)
	t.Setenv("SHELDON_TEST_BROWSER_FAIL", "1")
	a := startApp(t, env, "")
	_, token := readOpenState(t, a.target)
	a.cancel()
	err := <-a.done
	a.done <- err
	if err != nil {
		t.Fatal(err)
	}
	output := a.stdout.String() + a.stderr.String()
	if !strings.Contains(output, "Browser launch failed") || !strings.Contains(output, "instance.lock") || !strings.Contains(output, "http://127.0.0.1:") {
		t.Fatal("missing manual connection guidance")
	}
	if strings.Contains(output, token) || strings.Contains(output, "fake opener") {
		t.Fatal("unsafe browser error displayed")
	}
}

func TestCanceledLaunchDoesNotCreateStorage(t *testing.T) {
	env := newAppEnvironment(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, "", io.Discard, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(env.paths.Data); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled launch created storage")
	}
	if len(env.launches(t)) != 0 {
		t.Fatal("browser invoked")
	}
}

func TestSimultaneousLaunchesHaveOneOwner(t *testing.T) {
	env := newAppEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })
	const callers = 6
	done := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			done <- Run(ctx, "", io.Discard, io.Discard)
		})
	}
	targets := env.waitLaunches(t, callers)
	for _, target := range targets {
		if target != targets[0] {
			t.Fatal("simultaneous launches started multiple servers")
		}
	}
	for range callers - 1 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("forwarder stayed alive as another owner")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

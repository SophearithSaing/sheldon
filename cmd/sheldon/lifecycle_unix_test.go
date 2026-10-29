//go:build linux || darwin

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCLIProcessHelper(t *testing.T) {
	if os.Getenv("SHELDON_CLI_PROCESS_HELPER") != "1" {
		return
	}
	os.Args = []string{"sheldon"}
	if raw := os.Getenv("SHELDON_CLI_OPEN_URL"); raw != "" {
		os.Args = append(os.Args, "open", raw)
	}
	main()
}

func TestCLILaunchForwardAndSignalShutdown(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// No desktop opener is available in this fixture; never launch a real browser.
	env := append(os.Environ(), "SHELDON_CLI_PROCESS_HELPER=1", "SHELDON_CLI_OPEN_URL=", "HOME="+root,
		"XDG_DATA_HOME="+filepath.Join(root, "data"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR="+filepath.Join(root, "runtime"), "PATH="+root, "SHELDON_DEV_ORIGIN=", "SHELDON_DEV_LISTEN=")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	owner := exec.CommandContext(ctx, executable, "-test.run=^TestCLIProcessHelper$")
	owner.Env = env
	var diagnostics bytes.Buffer
	owner.Stderr = &diagnostics
	stdout, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { owner.Process.Kill(); owner.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	var origin string
	select {
	case line := <-ready:
		origin = strings.TrimSpace(strings.TrimPrefix(line, "Sheldon: "))
	case <-ctx.Done():
		t.Fatal("CLI did not become ready")
	}
	if !strings.HasPrefix(origin, "http://127.0.0.1:") {
		t.Fatalf("unexpected CLI output %q", origin)
	}
	dataDir := filepath.Join(root, "data", "sheldon")
	if runtime.GOOS == "darwin" {
		dataDir = filepath.Join(root, "Library", "Application Support", "sheldon")
	}
	recordPath := filepath.Join(dataDir, "instance.lock")
	b, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct{ URL, Token string }
	if json.Unmarshal(b, &state) != nil || state.URL != origin || state.Token == "" {
		t.Fatal("CLI did not publish discovery")
	}
	forwarder := exec.CommandContext(ctx, executable, "-test.run=^TestCLIProcessHelper$")
	forwarder.Env = append(env, "SHELDON_CLI_OPEN_URL=https://github.com/owner/repo/pull/7")
	forwardOutput, err := forwarder.CombinedOutput()
	if err != nil {
		t.Fatalf("second CLI failed: %v", err)
	}
	if !strings.Contains(string(forwardOutput), origin) || strings.Contains(string(forwardOutput), state.Token) {
		t.Fatal("forwarding output is unsafe or points at another server")
	}
	if err := owner.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err != nil {
		t.Fatalf("signal shutdown failed: %v", err)
	}
	if !strings.Contains(diagnostics.String(), "Browser launch failed") || strings.Contains(diagnostics.String(), state.Token) {
		t.Fatal("browser failure did not provide safe guidance")
	}
	b, err = os.ReadFile(recordPath)
	if err != nil || len(b) != 0 {
		t.Fatal("CLI shutdown left discovery credentials")
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(origin, "http://"), 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("CLI shutdown left listener open")
	}
}

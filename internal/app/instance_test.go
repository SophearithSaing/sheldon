package app

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/platform"
)

func testPaths(t *testing.T) platform.Paths {
	t.Helper()
	root := t.TempDir()
	return platform.Paths{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Runtime: filepath.Join(root, "runtime")}
}

func TestOSLockReleasedAfterProcessDeath(t *testing.T) {
	paths := testPaths(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestLockProcessHelper$")
	cmd.Env = append(os.Environ(), "SHELDON_TEST_LOCK_DIR="+paths.Data)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("helper failed: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not acquire lock")
	}
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	if owned, err := i.acquire(); err != nil || owned {
		t.Fatal("two processes acquired the same writer lock")
	}
	if _, err := i.read(); err != nil {
		t.Fatal("contender could not read discovery while owner held lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatalf("OS did not release crashed owner's lock: %v", err)
	}
}

func TestLockProcessHelper(t *testing.T) {
	path := os.Getenv("SHELDON_TEST_LOCK_DIR")
	if path == "" {
		return
	}
	i, err := openInstance(path)
	if err != nil {
		os.Exit(10)
	}
	defer i.close()
	if owned, err := i.acquire(); err != nil || !owned {
		os.Exit(11)
	}
	if err := i.publish(record{URL: "http://127.0.0.1:1", Token: "fixture"}); err != nil {
		os.Exit(12)
	}
	io.WriteString(os.Stdout, "locked\n")
	io.Copy(io.Discard, os.Stdin)
}

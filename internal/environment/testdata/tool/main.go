// This executable is an offline metadata fixture, never a real harness.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// main records every invocation and rejects anything except fixed version/help calls.
func main() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	args := strings.Join(os.Args[1:], " ")
	dir, _ := os.Getwd()
	if expected := os.Getenv("SHELDON_ENV_TEST_EXPECT_ROUTE"); expected != "" && os.Getenv("OPENAI_BASE_URL") != expected {
		os.Exit(92)
	}
	input, _ := io.ReadAll(os.Stdin)
	if root := os.Getenv("SHELDON_ENV_TEST_LOG"); root != "" {
		data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "dir": dir, "stdin": len(input)})
		file, err := os.OpenFile(filepath.Join(root, name+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(90)
		}
		file.Write(append(data, '\n'))
		file.Close()
	}
	versions := map[string]string{"git": "git version 2.45.1", "gh": "gh version 2.45.0 (fixture)", "opencode": "2.1.4", "codex": "codex-cli 0.100.0", "claude": "2.1.100 (Claude Code)"}
	help := map[string]string{"opencode": "serve --hostname <host> --port <port> --stdio --service", "codex": "app-server --listen <transport>", "claude": "--print --output-format stream-json --include-partial-messages --verbose --resume <id> --permission-mode <mode> --disallowedTools <tools>"}
	allowedHelp := map[string]string{"opencode": "serve --help", "codex": "app-server --help", "claude": "--help"}
	version := args == "--version"
	if versions[name] == "" || (!version && args != allowedHelp[name] && !(name == "codex" && args == "--help")) {
		fmt.Fprintln(os.Stderr, "forbidden fixture invocation")
		os.Exit(91)
	}
	mode := os.Getenv("SHELDON_ENV_TEST_" + strings.ToUpper(name))
	if mode == "fail" || (mode == "fail-help" && !version) {
		fmt.Fprintln(os.Stderr, "private-provider-secret-do-not-display")
		os.Exit(7)
	}
	if mode == "hang" || (mode == "hang-help" && !version) {
		time.Sleep(30 * time.Second)
	}
	if mode == "overflow" {
		fmt.Print(strings.Repeat("x", 65537))
		return
	}
	if mode == "stderr-overflow" {
		fmt.Fprint(os.Stderr, strings.Repeat("x", 65537))
		return
	}
	if version {
		switch mode {
		case "legacy":
			fmt.Println("1.9.9")
		case "future":
			fmt.Println("3.0.0")
		case "prerelease":
			fmt.Println("2.1.4-private-provider-secret")
		case "bad-version":
			fmt.Println("private-provider-secret-do-not-display")
		default:
			fmt.Println(versions[name])
		}
		return
	}
	if mode == "hidden-help" {
		fmt.Println("Additional options are not listed.")
		return
	}
	if name == "codex" && args == "--help" {
		fmt.Println("--sandbox <policy> --ask-for-approval <policy>")
	} else {
		fmt.Println(help[name])
	}
	// An untrusted help URL must never become a network destination.
	fmt.Println(os.Getenv("SHELDON_ENV_TEST_SERVICE"))
	fmt.Fprintln(os.Stderr, "private-provider-secret-do-not-display")
}

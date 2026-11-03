package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// main audits production argv, then substitutes an offline transport only in this fixture.
func main() {
	args := append([]string{}, os.Args[1:]...)
	config := make(map[string]string)
	index := 0
	for index+1 < len(args) && args[index] == "-c" {
		key, value, _ := strings.Cut(args[index+1], "=")
		config[key] = value
		index += 2
	}
	if index == len(args) {
		os.Exit(90)
	}
	operation := args[index]
	want := map[string]string{"core.fsmonitor": "false", "protocol.allow": "never", "protocol.https.allow": "always", "protocol.ssh.allow": "always", "http.sslVerify": "true", "http.followRedirects": "false", "maintenance.auto": "false", "gc.auto": "0", "submodule.recurse": "false", "fetch.recurseSubmodules": "false", "fetch.writeCommitGraph": "false"}
	safe := config["core.hooksPath"] != "" && config["core.attributesFile"] != "" && config["protocol.file.allow"] == ""
	for key, value := range want {
		safe = safe && config[key] == value
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_EXEC_PATH"} {
		safe = safe && os.Getenv(key) == ""
	}
	safe = safe && os.Getenv("GIT_TERMINAL_PROMPT") == "0" && os.Getenv("GCM_INTERACTIVE") == "never" && os.Getenv("GIT_TRACE2_EVENT") == "0"
	safe = safe && os.Getenv("GIT_ALLOW_PROTOCOL") == "https:ssh"
	input, _ := io.ReadAll(os.Stdin)
	safe = safe && len(input) == 0
	remoteIndex := -1
	if operation == "fetch" || operation == "ls-remote" {
		for n := index + 1; n+1 < len(args); n++ {
			if args[n] == "--" {
				remoteIndex = n + 1
				break
			}
		}
		safe = safe && remoteIndex >= 0 && strings.HasPrefix(args[remoteIndex], "https://github.com/") && strings.HasSuffix(args[remoteIndex], ".git")
	}
	if operation == "fetch" {
		for _, flag := range []string{"--no-tags", "--no-recurse-submodules", "--no-auto-maintenance", "--atomic", "--no-write-fetch-head", "--no-prune", "--no-prune-tags", "--refmap=", "--no-progress"} {
			found := false
			for _, arg := range args[index+1 : remoteIndex] {
				if arg == flag {
					found = true
				}
			}
			safe = safe && found
		}
		for _, spec := range args[remoteIndex+1:] {
			_, destination, found := strings.Cut(spec, ":")
			safe = safe && found && strings.HasPrefix(destination, "refs/sheldon/")
		}
	}
	if operation == "diff" {
		for _, flag := range []string{"--raw", "-z", "--patch", "--no-abbrev", "--full-index", "--no-ext-diff", "--no-textconv", "--no-relative", "--ignore-submodules=none", "--submodule=short"} {
			found := false
			for _, arg := range args[index+1:] {
				if arg == flag {
					found = true
				}
			}
			safe = safe && found
		}
	}
	wd, _ := os.Getwd()
	audit, _ := json.Marshal(struct {
		Operation, Directory          string
		Safe, AuthenticationPreserved bool
		PID                           int
	}{operation, wd, safe, os.Getenv("GIT_SSH_COMMAND") == "preserved-fixture-ssh-route", os.Getpid()})
	log, err := os.OpenFile(os.Getenv("SHELDON_GIT_AUDIT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(91)
	}
	log.Write(append(audit, '\n'))
	log.Close()
	if !safe {
		fmt.Fprint(os.Stderr, "unsafe fixture argv or environment")
		os.Exit(92)
	}
	mode := os.Getenv("SHELDON_GIT_MODE")
	if operation == "diff" {
		switch mode {
		case "diff-fail":
			fmt.Fprint(os.Stderr, "synthetic-secret private diagnostics")
			os.Exit(7)
		case "diff-overflow":
			fmt.Print(strings.Repeat("x", (4<<20)+1))
			return
		case "diff-truncated":
			fmt.Print(":100644")
			return
		case "diff-hang":
			for {
				time.Sleep(time.Second)
			}
		}
	}
	if operation == "cat-file" && len(args) > index+1 && args[index+1] == "blob" && mode == "materialize-hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	if operation == "fetch" {
		switch mode {
		case "fail":
			fmt.Fprint(os.Stderr, "synthetic-secret https://secret@invalid.example private diagnostics")
			os.Exit(7)
		case "overflow":
			fmt.Print(strings.Repeat("x", (4<<20)+1))
			return
		case "hang":
			for {
				time.Sleep(time.Second)
			}
		}
	}
	if remoteIndex >= 0 && mode != "native-transport" {
		args[remoteIndex] = os.Getenv("SHELDON_GIT_UPSTREAM")
		args = append([]string{"-c", "protocol.file.allow=always"}, args...)
	}
	command := exec.Command(os.Getenv("SHELDON_GIT_REAL"), args...)
	if remoteIndex >= 0 && mode != "native-transport" {
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, "GIT_ALLOW_PROTOCOL=") {
				command.Env = append(command.Env, variable)
			}
		}
		command.Env = append(command.Env, "GIT_ALLOW_PROTOCOL=file")
	}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		os.Exit(93)
	}
	if operation == "worktree" && mode == "worktree-fail" {
		os.Exit(7)
	}
	if operation == "fetch" && mode == "noncommit" {
		command := exec.Command(os.Getenv("SHELDON_GIT_REAL"), "-C", wd, "-c", "core.hooksPath="+filepath.Join(config["core.hooksPath"]), "update-ref", "refs/sheldon/pr/1/head", os.Getenv("SHELDON_GIT_TREE"))
		if command.Run() != nil {
			os.Exit(94)
		}
	}
}

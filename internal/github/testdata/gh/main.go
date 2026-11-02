package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"
)

// main implements a synthetic gh executable without network or authentication work.
func main() {
	wd, _ := os.Getwd()
	input, _ := io.ReadAll(os.Stdin)
	want := []string{"api", "--hostname", "github.com", "--method", "GET", "--include", "--header", "Accept: application/vnd.github+json", "repos/owner/repo/pulls/31"}
	audit := struct {
		Directory               string
		Arguments               bool
		StdinClosed             bool
		AuthenticationPreserved bool
		RestrictedEnvironment   bool
	}{
		Directory:               wd,
		Arguments:               reflect.DeepEqual(os.Args[1:], want),
		StdinClosed:             len(input) == 0,
		AuthenticationPreserved: os.Getenv("GH_TOKEN") == "synthetic-auth-token",
		RestrictedEnvironment:   os.Getenv("GH_HOST") == "github.com" && os.Getenv("GH_PROMPT_DISABLED") == "1" && os.Getenv("GH_PAGER") == "cat" && os.Getenv("PAGER") == "cat" && os.Getenv("GH_DEBUG") == "" && os.Getenv("GH_FORCE_TTY") == "" && os.Getenv("GH_NO_UPDATE_NOTIFIER") == "1" && os.Getenv("GH_NO_EXTENSION_UPDATE_NOTIFIER") == "1",
	}
	data, _ := json.Marshal(audit)
	if os.WriteFile(os.Getenv("SHELDON_TEST_GH_AUDIT"), data, 0600) != nil {
		os.Exit(90)
	}
	if !audit.Arguments || !audit.StdinClosed || !audit.AuthenticationPreserved || !audit.RestrictedEnvironment {
		os.Exit(91)
	}
	switch os.Getenv("SHELDON_TEST_GH_MODE") {
	case "hang":
		for {
			time.Sleep(time.Second)
		}
	case "large-stdout":
		fmt.Print(strings.Repeat("x", (1<<20)+1))
		return
	case "large-stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", (1<<20)+1))
		return
	case "auth":
		fmt.Fprint(os.Stderr, "synthetic-auth-token private-diagnostic https://secret@example.invalid")
		os.Exit(4)
	case "failed":
		fmt.Print(os.Getenv("SHELDON_TEST_GH_RESPONSE"))
		fmt.Fprint(os.Stderr, "synthetic-auth-token private-diagnostic https://secret@example.invalid")
		os.Exit(1)
	case "misleading-stderr":
		fmt.Fprint(os.Stderr, "401 authentication network private-diagnostic synthetic-auth-token")
		os.Exit(1)
	default:
		fmt.Print(os.Getenv("SHELDON_TEST_GH_RESPONSE"))
	}
}

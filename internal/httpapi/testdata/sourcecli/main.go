// This native executable supplies offline metadata and desktop-open capture for CLI browser tests.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// main dispatches only the installed fixture tool names; it never connects to GitHub or a desktop.
func main() {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) != 0 {
		os.Exit(90)
	}
	switch filepath.Base(os.Args[0]) {
	case "gh":
		metadata()
	case "open", "xdg-open":
		captureLaunch()
	default:
		os.Exit(91)
	}
}

// appendJSON records one private fixture event without printing it to command output.
func appendJSON(name string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		os.Exit(92)
	}
	file, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(92)
	}
	_, err = file.Write(append(data, '\n'))
	if closeErr := file.Close(); err != nil || closeErr != nil {
		os.Exit(92)
	}
}

// captureLaunch validates and privately captures only a real loopback review launch URL.
func captureLaunch() {
	if len(os.Args) != 2 {
		os.Exit(93)
	}
	value, err := url.Parse(os.Args[1])
	if err != nil || value.Scheme != "http" || value.Hostname() != "127.0.0.1" || value.Port() == "" || value.User != nil || value.RawQuery != "" {
		os.Exit(93)
	}
	id, ok := strings.CutPrefix(value.Path, "/reviews/")
	fragment, err := url.ParseQuery(value.Fragment)
	secret, decodeErr := base64.RawURLEncoding.DecodeString(fragment.Get("token"))
	if !ok || !githubpr.ValidID(id) || err != nil || len(fragment) != 1 || decodeErr != nil || len(secret) != 32 {
		os.Exit(93)
	}
	appendJSON(os.Getenv("SHELDON_SOURCE_LAUNCHES"), map[string]string{"url": value.String()})
}

// metadata serves only the two explicit PR payloads in a private, test-controlled manifest.
func metadata() {
	if len(os.Args) != 10 {
		os.Exit(94)
	}
	number, ok := strings.CutPrefix(os.Args[9], "repos/fixture/repo/pulls/")
	want := []string{"api", "--hostname", "github.com", "--method", "GET", "--include", "--header", "Accept: application/vnd.github+json", "repos/fixture/repo/pulls/" + number}
	if !ok || number != "31" && number != "32" || !reflect.DeepEqual(os.Args[1:], want) || os.Getenv("GH_HOST") != "github.com" || os.Getenv("GH_PROMPT_DISABLED") != "1" || os.Getenv("GH_DEBUG") != "" {
		os.Exit(94)
	}
	var fixture struct {
		PRs      map[string]json.RawMessage `json:"prs"`
		Fallback map[string]bool            `json:"fallback"`
	}
	data, err := os.ReadFile(os.Getenv("SHELDON_SOURCE_METADATA"))
	if err != nil || json.Unmarshal(data, &fixture) != nil || len(fixture.PRs[number]) == 0 {
		os.Exit(95)
	}
	appendJSON(os.Getenv("SHELDON_SOURCE_GH_AUDIT"), map[string]any{"number": number, "safe": true})
	if fixture.Fallback[number] {
		fmt.Print("HTTP/2.0 401 Unauthorized\r\nContent-Type: application/json\r\n\r\n{}")
		os.Exit(4)
	}
	fmt.Print("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n" + string(fixture.PRs[number]))
}

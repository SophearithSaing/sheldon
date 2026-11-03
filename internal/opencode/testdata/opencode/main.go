// Command opencode is an offline native-protocol fixture, never an inference harness.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

var auditMu sync.Mutex

// audit records synthetic fixture input without local transport credentials.
func audit(value any) {
	auditMu.Lock()
	defer auditMu.Unlock()
	f, err := os.OpenFile(os.Getenv("SHELDON_NATIVE_AUDIT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		json.NewEncoder(f).Encode(value)
		f.Close()
	}
}

// main serves only the small versioned metadata/session/permission fixture protocol.
func main() {
	mode := os.Getenv("SHELDON_NATIVE_MODE")
	dir, _ := os.Getwd()
	audit(map[string]any{"args": os.Args[1:], "directory": dir, "restricted": os.Getenv("OPENCODE_CONFIG_PROJECT_DISABLE") == "1" && os.Getenv("OPENCODE_DISABLE_CLAUDE_CODE") == "1" && os.Getenv("OPENCODE_DISABLE_AUTOUPDATE") == "1", "passwordLength": len(os.Getenv("OPENCODE_PASSWORD")), "config": json.RawMessage(os.Getenv("OPENCODE_CONFIG_CONTENT")), "authPreserved": os.Getenv("SHELDON_SYNTHETIC_AUTH") == "fixture-route"})
	if reflect.DeepEqual(os.Args[1:], []string{"--version"}) {
		switch mode {
		case "version-wrong":
			fmt.Println("opencode v2.1.5")
		case "version-fail":
			fmt.Fprintln(os.Stderr, "synthetic secret in native diagnostic")
			os.Exit(1)
		case "version-overflow":
			fmt.Print(strings.Repeat("x", 70<<10))
		case "version-hang":
			listener, _ := net.Listen("tcp4", "127.0.0.1:0")
			listener.Accept()
		default:
			fmt.Println("opencode v2.1.4")
		}
		return
	}
	if !reflect.DeepEqual(os.Args[1:], []string{"serve", "--stdio", "--hostname", "127.0.0.1", "--port", "0"}) {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	endpoint := "http://" + listener.Addr().String()
	switch mode {
	case "early-exit":
		return
	case "bad-announcement":
		fmt.Println("not JSON: synthetic secret")
	case "oversize-announcement":
		fmt.Print(strings.Repeat("x", 70<<10))
	case "foreign-endpoint":
		fmt.Println(`{"url":"http://example.invalid:7331"}`)
	case "announcement-hang":
	default:
		json.NewEncoder(os.Stdout).Encode(map[string]string{"url": endpoint})
	}
	done := make(chan struct{})
	trigger := make(chan struct{}, 1)
	var mu sync.Mutex
	sessions := make(map[string]map[string]any)
	defaultReads := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "opencode" || password != os.Getenv("OPENCODE_PASSWORD") || mode == "bad-auth" {
			w.WriteHeader(401)
			fmt.Fprint(w, "synthetic secret")
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		audit(map[string]any{"method": r.Method, "path": r.URL.Path, "body": string(body), "directory": r.URL.Query().Get("location[directory]")})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/info":
			if mode == "info-wrong" {
				fmt.Fprint(w, `{"version":"2.2.0"}`)
			} else if mode == "http-redirect" {
				http.Redirect(w, r, "http://example.invalid/secret", 302)
			} else {
				fmt.Fprint(w, `{"version":"2.1.4"}`)
			}
		case r.URL.Path == "/api/event":
			if mode == "event-content-type" {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"server.connected\"}\n\n")
			w.(http.Flusher).Flush()
			if mode == "stream-loss" {
				return
			}
			if mode == "event-overflow" {
				fmt.Fprint(w, ":"+strings.Repeat("x", 2<<20)+"\n\n")
				return
			}
			fmt.Fprint(w, "data: {\"type\":\"session.tool.input.delta\",\"data\":{\"delta\":{\"structured\":true}}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-trigger:
				fmt.Fprint(w, "data: {\"type\":\"permission.asked\",\"data\":{\"sessionID\":\"ses_fixture\"}}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			case <-done:
			case <-r.Context().Done():
			}
		case r.URL.Path == "/api/model/default":
			mu.Lock()
			defaultReads++
			changed := mode == "default-changes" && defaultReads > 1
			mu.Unlock()
			if mode == "approval" {
				trigger <- struct{}{}
			}
			switch mode {
			case "model-missing":
				fmt.Fprint(w, `{"data":null}`)
			case "model-unavailable":
				w.WriteHeader(404)
				fmt.Fprint(w, "synthetic secret")
			case "model-overflow":
				fmt.Fprint(w, strings.Repeat("x", 9<<20))
			case "model-trailing":
				fmt.Fprint(w, `{"data":{"providerID":"fixture","id":"chat"}} {}`)
			default:
				if changed {
					fmt.Fprint(w, `{"data":{"providerID":"fixture","id":"changed-default"}}`)
				} else {
					fmt.Fprint(w, `{"data":{"providerID":"fixture","id":"chat"}}`)
				}
			}
		case r.Method == "POST" && r.URL.Path == "/api/session":
			if mode == "create-hang" {
				os.WriteFile(os.Getenv("SHELDON_NATIVE_CONTROL")+".started", []byte("started"), 0600)
				<-r.Context().Done()
				return
			}
			var input map[string]any
			json.Unmarshal(body, &input)
			mu.Lock()
			id := "ses_fixture"
			if len(sessions) > 0 && mode != "duplicate-session" {
				id += "_" + strconv.Itoa(len(sessions)+1)
			}
			session := map[string]any{"id": id, "location": input["location"], "permissions": input["permissions"], "model": input["model"]}
			sessions[id] = session
			switch mode {
			case "session-model":
				session["model"] = map[string]string{"providerID": "other", "id": "fallback"}
			case "session-location":
				session["location"] = map[string]string{"directory": "/foreign"}
			case "session-permissions":
				session["permissions"] = []any{}
			case "session-id":
				session["id"] = "../foreign"
			}
			json.NewEncoder(w).Encode(map[string]any{"data": session})
			mu.Unlock()
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/session/"):
			mu.Lock()
			session := sessions[strings.TrimPrefix(r.URL.Path, "/api/session/")]
			control, _ := os.ReadFile(os.Getenv("SHELDON_NATIVE_CONTROL"))
			if string(control) == "missing-session" || session == nil {
				mu.Unlock()
				w.WriteHeader(404)
				return
			}
			if mode == "get-mismatch" || string(control) == "changed-session" {
				session["model"] = map[string]string{"providerID": "other", "id": "changed"}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": session})
			mu.Unlock()
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/permission"):
			var input struct {
				Action    string   `json:"action"`
				Resources []string `json:"resources"`
			}
			json.Unmarshal(body, &input)
			effect := "deny"
			if input.Action == "glob" || input.Action == "grep" || input.Action == "read" && !strings.Contains(input.Resources[0], ".env") {
				effect = "allow"
			}
			if mode == "permission-mismatch" && input.Action == "shell" {
				effect = "allow"
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"effect": effect}})
		default:
			w.WriteHeader(404)
		}
	})
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	io.Copy(io.Discard, os.Stdin)
	close(done)
	server.Shutdown(context.Background())
}

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
	"time"
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
	audit(map[string]any{"args": os.Args[1:], "directory": dir, "pid": os.Getpid(), "restricted": os.Getenv("OPENCODE_CONFIG_PROJECT_DISABLE") == "1" && os.Getenv("OPENCODE_DISABLE_CLAUDE_CODE") == "1" && os.Getenv("OPENCODE_DISABLE_AUTOUPDATE") == "1", "passwordLength": len(os.Getenv("OPENCODE_PASSWORD")), "config": json.RawMessage(os.Getenv("OPENCODE_CONFIG_CONTENT")), "authPreserved": os.Getenv("SHELDON_SYNTHETIC_AUTH") == "fixture-route"})
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
	execution := newExecutionFixture(mode, done)
	trigger := make(chan struct{}, 1)
	var mu sync.Mutex
	sessions := make(map[string]map[string]any)
	readFixtureState("sessions", &sessions)
	defaultReads := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "opencode" || password != os.Getenv("OPENCODE_PASSWORD") || mode == "bad-auth" {
			w.WriteHeader(401)
			fmt.Fprint(w, "synthetic secret")
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		audit(map[string]any{"method": r.Method, "path": r.URL.Path, "body": string(body), "directory": r.URL.Query().Get("location[directory]"), "pid": os.Getpid()})
		w.Header().Set("Content-Type", "application/json")
		if execution.handle(w, r, body) {
			return
		}
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
			for {
				select {
				case event := <-execution.events:
					if event == "close-feed" {
						return
					}
					frame := "data: " + event + "\n\n"
					if mode == "answer-fragmented" {
						for len(frame) > 0 {
							n := min(7, len(frame))
							fmt.Fprint(w, frame[:n])
							frame = frame[n:]
							w.(http.Flusher).Flush()
						}
					} else {
						fmt.Fprint(w, frame)
						w.(http.Flusher).Flush()
					}
				case <-trigger:
					fmt.Fprint(w, "data: {\"type\":\"permission.asked\",\"data\":{\"sessionID\":\"ses_fixture\"}}\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				case <-done:
					return
				case <-r.Context().Done():
					return
				}
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
			case "model-default-variant":
				fmt.Fprint(w, `{"data":{"providerID":"fixture","id":"chat","variant":"default"}}`)
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
				if changed || mode == "reopen-changed-default" {
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
			if mode == "create-gated" {
				os.WriteFile(os.Getenv("SHELDON_NATIVE_CONTROL")+".created", []byte("created"), 0600)
				tick := time.NewTicker(10 * time.Millisecond)
				waiting := true
				for waiting {
					select {
					case <-r.Context().Done():
						tick.Stop()
						mu.Unlock()
						return
					case <-tick.C:
						data, _ := os.ReadFile(os.Getenv("SHELDON_NATIVE_CONTROL"))
						waiting = string(data) != "release-create"
					}
				}
				tick.Stop()
			}
			switch mode {
			case "session-default-variant", "model-default-variant":
				session["model"] = map[string]string{"providerID": "fixture", "id": "chat", "variant": "default"}
			case "session-unsupported-variant":
				session["model"] = map[string]string{"providerID": "fixture", "id": "chat", "variant": "other"}
			case "session-model":
				session["model"] = map[string]string{"providerID": "other", "id": "fallback"}
			case "session-location":
				session["location"] = map[string]string{"directory": "/foreign"}
			case "session-permissions":
				session["permissions"] = []any{}
			case "session-id":
				session["id"] = "../foreign"
			}
			writeFixtureState("sessions", sessions)
			execution.mu.Lock()
			location, _ := input["location"].(map[string]any)
			execution.directories[id], _ = location["directory"].(string)
			execution.mu.Unlock()
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
			if string(control) == "changed-location" {
				session["location"] = map[string]string{"directory": "/foreign"}
			}
			if string(control) == "changed-permissions" {
				session["permissions"] = []any{}
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
			if input.Action == "shell" {
				mu.Lock()
				id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/session/"), "/permission")
				permissions, _ := sessions[id]["permissions"].([]any)
				for _, raw := range permissions {
					permission, _ := raw.(map[string]any)
					if permission["action"] == "shell" && wildcardMatch(permission["resource"].(string), input.Resources[0]) {
						effect, _ = permission["effect"].(string)
					}
				}
				mu.Unlock()
			}
			control, _ := os.ReadFile(os.Getenv("SHELDON_NATIVE_CONTROL"))
			if (mode == "permission-mismatch" || string(control) == "permission-mismatch") && input.Action == "shell" {
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

package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:1", "http://127.0.0.1:65535"} {
		if !validEndpoint(endpoint) {
			t.Fatal("canonical loopback endpoint rejected")
		}
	}
	for _, endpoint := range []string{"http://localhost:1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:01", "http://127.0.0.1:1/", "http://127.0.0.1:1?", "http://127.0.0.1:1#token", "http://user@127.0.0.1:1", "https://127.0.0.1:1", "http://[::1]:1", "http://example.invalid:1", "http://127.0.0.1:1/path"} {
		if validEndpoint(endpoint) {
			t.Fatal("unsafe native endpoint accepted")
		}
	}
}

func TestFiniteHTTPBodyCancellationAndNoRedirect(t *testing.T) {
	for _, mode := range []string{"trailing", "invalid UTF8", "redirect", "stalled body", "service cancellation"} {
		t.Run(mode, func(t *testing.T) {
			lifetime, cancelService := context.WithCancelCause(context.Background())
			defer cancelService(context.Canceled)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "opencode" || password != "synthetic-transport" {
					t.Error("Basic authentication missing")
				}
				switch mode {
				case "trailing":
					fmt.Fprint(w, `{} {}`)
				case "invalid UTF8":
					w.Write([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})
				case "redirect":
					http.Redirect(w, r, "http://example.invalid/synthetic-secret", 302)
				default:
					fmt.Fprint(w, "{")
					w.(http.Flusher).Flush()
					if mode == "service cancellation" {
						cancelService(ErrRestricted)
					}
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			s := &Service{ctx: lifetime, cancel: cancelService, client: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: server.URL, password: "synthetic-transport"}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var out any
			err := s.call(ctx, "GET", "/finite", nil, &out)
			if err == nil || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "example.invalid") {
				t.Fatal("HTTP failure exposed raw data or became success")
			}
			if mode == "stalled body" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("body deadline cause lost")
			}
		})
	}
}

func TestSSEBoundsAndStructuredPayloads(t *testing.T) {
	var data []string
	err := readSSE(strings.NewReader(": heartbeat\r\nevent: native\r\ndata: {\"type\":\"unknown\",\r\ndata: \"delta\":{\"structured\":true}}\r\n\r\n"), func(value []byte) error { data = append(data, string(value)); return nil })
	if !errors.Is(err, ErrStreamLost) || len(data) != 1 || !strings.Contains(data[0], "structured") {
		t.Fatal("fragmented/structured SSE handling failed")
	}
	for _, frame := range []string{"data: " + strings.Repeat("x", maxFrame) + "\n\n", strings.Repeat(": ignored comment\n", maxFrame/10) + "\n", "event: effect/http-api/stream/failure\n\n"} {
		called := false
		err := readSSE(strings.NewReader(frame), func([]byte) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("oversized/failed native frame emitted data")
		}
	}
}

func TestModelShapeAndNativeIDs(t *testing.T) {
	for _, model := range []Model{{}, {ProviderID: "fixture", ID: ""}, {ProviderID: "fixture", ID: "https://secret"}, {ProviderID: "fixture", ID: "chat model"}, {ProviderID: "fixture", ID: "chat", Variant: "unsupported"}} {
		if validModel(model) {
			t.Fatal("unresolved or unsupported model accepted")
		}
	}
	for _, id := range []string{"", "../session", "native/session", strings.Repeat("x", 201)} {
		if validNativeID(id) {
			t.Fatal("invalid native ID accepted")
		}
	}
}

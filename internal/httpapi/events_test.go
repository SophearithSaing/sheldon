package httpapi

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func readFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String()
		}
	}
}

func TestFixtureSSEStreamsAndShutsDown(t *testing.T) {
	s, client := newTestServer(t, "")
	r := request(t, s, client, "GET", "/api/fixture/events", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	if r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("fixture stream was not established")
	}
	reader := bufio.NewReader(r.Body)
	if frame := readFrame(t, reader); frame != "event: fixture\ndata: {\"fixture\":\"connected\"}\n\n" {
		t.Fatalf("unexpected initial frame %q", frame)
	}
	if frame := readFrame(t, reader); frame != "event: fixture\ndata: {\"fixture\":\"heartbeat\"}\n\n" {
		t.Fatalf("unexpected live frame %q", frame)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("shutdown did not finish active stream: %v", err)
	}
}

func TestFixtureSSEAcceptsSameOriginBearer(t *testing.T) {
	s, client := newTestServer(t, "")
	token := s.APIToken()
	r := request(t, s, client, "GET", "/api/fixture/events", "", map[string]string{"Authorization": "Bearer " + token, "Origin": s.URL(), "Sec-Fetch-Site": "same-origin"})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("same-origin SSE status %d", r.StatusCode)
	}
	if !strings.Contains(readFrame(t, bufio.NewReader(r.Body)), `"fixture":"connected"`) {
		t.Fatal("same-origin bearer request did not receive fixture events")
	}
	r.Body.Close()
}

func TestDisconnectedStreamReleasesHandler(t *testing.T) {
	s, err := Listen("")
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{}, 1)
	s.http.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	client := startTestServer(t, s)
	r := request(t, s, client, "GET", "/api/fixture/events", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	readFrame(t, bufio.NewReader(r.Body))
	r.Body.Close()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected SSE handler remained active")
	}
	// Disconnecting one reader must not stop the server.
	health := request(t, s, client, "GET", "/api/health", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	if health.StatusCode != http.StatusOK {
		t.Fatal("stream disconnect stopped server")
	}
}

func TestShutdownDeadlineClosesStalledRequest(t *testing.T) {
	s, err := Listen("")
	if err != nil {
		t.Fatal(err)
	}
	// A test-only body reader keeps a request active without a product mutation.
	s.mux.HandleFunc("POST /api/test/stalled", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
	})
	active := make(chan struct{}, 1)
	s.http.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateActive {
			select {
			case active <- struct{}{}:
			default:
			}
		}
	}
	startTestServer(t, s)
	conn, err := net.Dial("tcp", s.authority)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Declare a body but never send it. Shutdown must not wait for ReadTimeout.
	_, err = io.WriteString(conn, "POST /api/test/stalled HTTP/1.1\r\nHost: "+s.authority+"\r\nAuthorization: Bearer "+s.APIToken()+"\r\nContent-Length: 100\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-active:
	case <-time.After(time.Second):
		t.Fatal("stalled request never became active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = s.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled request did not exercise forced shutdown: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("stalled request survived shutdown")
	}
}

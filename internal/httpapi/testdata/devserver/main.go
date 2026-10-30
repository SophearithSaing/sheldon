// This executable is a browser-test fixture, not a CLI or release startup path.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
)

// main starts the browser-test backend and writes its credentials to private storage.
func main() {
	s, err := httpapi.Listen(os.Getenv("SHELDON_DEV_LISTEN"), os.Getenv("SHELDON_DEV_ORIGIN"))
	if err != nil {
		os.Exit(1)
	}
	state, _ := json.Marshal(map[string]string{"url": s.URL(), "token": s.APIToken()})
	if os.WriteFile(os.Getenv("SHELDON_TEST_STATE"), state, 0600) != nil {
		s.Shutdown(context.Background())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	fmt.Fprintln(os.Stdout, "backend-ready")
	<-ctx.Done()
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	s.Shutdown(shutdown)
	cancel()
	<-done
}

// Package app owns Sheldon's local CLI server lifetime and instance handoff.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// Run starts or forwards to the local runtime.
func Run(ctx context.Context, prURL string, stdout, stderr io.Writer) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prURL != "" {
		if _, err := githubpr.ParseURL(prURL); err != nil {
			return err
		}
	}
	paths, err := platform.Resolve()
	if err != nil {
		return err
	}
	devOrigin := os.Getenv("SHELDON_DEV_ORIGIN")
	if devOrigin != "" {
		if err := httpapi.ValidateOrigin(devOrigin); err != nil {
			return err
		}
	}
	i, err := openInstance(paths.Data)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, i.close()) }()
	startup, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var retryDelay time.Duration
	for {
		select {
		case <-startup.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("another Sheldon instance holds the writer lock but is unavailable or unverified; retry after it exits (do not delete instance.lock while it is running)")
		case <-time.After(retryDelay):
		}
		// The first attempt is immediate; failures wait before trying again.
		retryDelay = 50 * time.Millisecond

		owned, err := i.acquire()
		if err != nil {
			return err
		}
		if owned {
			cancel()
			return serve(ctx, paths, i, prURL, devOrigin, stdout, stderr)
		}
		record, err := i.read()
		if err != nil {
			continue
		}
		client, err := httpapi.NewClient(record.URL, record.Token)
		if err != nil {
			continue
		}
		if err := client.Verify(startup); err != nil {
			continue
		}
		if devOrigin != "" && record.DevOrigin != devOrigin {
			return errors.New("the running instance uses a different UI origin; stop it before starting development mode")
		}

		route := "/"
		if prURL != "" {
			// Never retry an open with unknown delivery after a network error.
			opened, err := client.Open(startup, prURL)
			if err != nil {
				return err
			}
			route = opened.Route
		}
		return launch(ctx, paths, client, record.URL, record.DevOrigin, route, stdout, stderr)
	}
}

// serve starts the HTTP server and publishes instance discovery.
func serve(ctx context.Context, paths platform.Paths, i *instance, prURL, devOrigin string, stdout, stderr io.Writer) (result error) {
	route := "/"
	if prURL != "" {
		pr, err := i.RegisterPR(prURL)
		if err != nil {
			return err
		}
		route, _ = githubpr.ReviewPath(pr.ID)
	}
	address := ""
	if devOrigin != "" {
		address = os.Getenv("SHELDON_DEV_LISTEN")
		if address == "" {
			address = "127.0.0.1:7331"
		}
	}
	s, err := httpapi.Listen(address, devOrigin, &sourceRegistry{instance: i, paths: paths})
	if err != nil {
		return err
	}
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = s.Serve()
		close(done)
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result = errors.Join(result, s.Shutdown(shutdown))
		<-done
	}()
	if err := i.publish(record{URL: s.URL(), Token: s.APIToken(), DevOrigin: devOrigin}); err != nil {
		return err
	}
	client, err := httpapi.NewClient(s.URL(), s.APIToken())
	if err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = client.Verify(ready)
	cancel()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "API token: %s\n", s.APIToken())
	if err := launch(ctx, paths, client, s.URL(), devOrigin, route, stdout, stderr); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return nil
	case <-done:
		if serveErr != nil {
			return serveErr
		}
		return errors.New("local HTTP server stopped unexpectedly")
	}
}

// launch opens the browser with a token-bearing launch URL.
func launch(ctx context.Context, paths platform.Paths, client *httpapi.Client, origin, devOrigin, route string, stdout, stderr io.Writer) error {
	target, err := client.BrowserURL(route)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Sheldon: %s\n", origin)
	browserCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if devOrigin != "" {
		target, err = client.DevelopmentURL(browserCtx, devOrigin, route)
		if err != nil {
			fmt.Fprintln(stderr, "Development UI is unavailable or unverified. Start Vite at the configured development origin, then rerun the original sheldon command. The backend remains running.")
			return nil
		}
		origin = devOrigin
		fmt.Fprintf(stdout, "Development UI: %s\n", origin)
	}
	err = openBrowser(browserCtx, target)
	if err != nil {
		// Browser/OS errors may include the launch URL, so do not print them.
		fmt.Fprintf(stderr, "Browser launch failed. Open %s%s and enter the token field from %s, or rerun the original sheldon command.\n", origin, route, filepath.Join(paths.Data, "instance.lock"))
	}
	return nil
}

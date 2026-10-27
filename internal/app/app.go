// Package app owns Sheldon's local CLI server lifetime and instance handoff.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// Run starts the local runtime or forwards to its verified current owner.
// An empty PR URL opens the runtime page without changing the pending PR intent.
// Nonempty PR URLs must already be normalized and validated by the caller.
func Run(ctx context.Context, prURL string, stdout, stderr io.Writer) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths, err := platform.Resolve()
	if err != nil {
		return err
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
			return serve(ctx, paths, i, prURL, stdout, stderr)
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

		if prURL != "" {
			// Never retry an open with unknown delivery after a network error.
			if err := client.Open(startup, prURL); err != nil {
				return err
			}
		}
		return launch(ctx, paths, client, record.URL, stdout, stderr)
	}
}

// serve publishes discovery only after binding, then drains HTTP before releasing ownership.
func serve(ctx context.Context, paths platform.Paths, i *instance, prURL string, stdout, stderr io.Writer) (result error) {
	s, err := httpapi.Listen("")
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
	if prURL != "" {
		if err := s.SetOpenURL(prURL); err != nil {
			return err
		}
	}
	if err := i.publish(record{URL: s.URL(), Token: s.APIToken()}); err != nil {
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
	if err := launch(ctx, paths, client, s.URL(), stdout, stderr); err != nil {
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

// launch opens the browser without logging credentials and leaves failures actionable.
func launch(ctx context.Context, paths platform.Paths, client *httpapi.Client, origin string, stdout, stderr io.Writer) error {
	target, err := client.BrowserURL()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Sheldon: %s\n", origin)
	browserCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = openBrowser(browserCtx, target)
	cancel()
	if err != nil {
		// Browser/OS errors may include the launch URL, so do not print them.
		fmt.Fprintf(stderr, "Browser launch failed. Open %s and enter the token field from %s, or run sheldon again to retry.\n", origin, filepath.Join(paths.Data, "instance.lock"))
	}
	return nil
}

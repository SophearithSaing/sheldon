package app

import (
	"context"
	"errors"
	"testing"
)

func TestBrowserCancellationDoesNotInvokeDesktop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := openBrowser(ctx, "http://127.0.0.1:1234/"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestBrowserRejectsInvalidWindowsURL(t *testing.T) {
	if err := openBrowser(context.Background(), "http://127.0.0.1:1234/\x00"); err == nil {
		t.Fatal("URL containing NUL accepted")
	}
}

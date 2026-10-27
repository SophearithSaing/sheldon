package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/SophearithSaing/sheldon/internal/app"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

var version = "dev"

const usage = `Sheldon — Don't just merge it. Understand it.

Usage:
  sheldon
  sheldon open <github-pr-url>
  sheldon --help
  sheldon --version

Starts the local runtime; Ctrl+C stops it. PR loading and the application UI are not implemented yet.
`

// main exits with the CLI status after releasing signal handlers and runtime resources.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, app.Run)
	stop()
	os.Exit(code)
}

// run validates CLI arguments before starting or contacting the local runtime.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, start func(context.Context, string, io.Writer, io.Writer) error) int {
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return 0
		case "--version":
			fmt.Fprintf(stdout, "sheldon %s\n", version)
			return 0
		}
	}
	var prURL string
	if len(args) != 0 {
		if len(args) != 2 || args[0] != "open" {
			fmt.Fprintln(stderr, "Unsupported command or arguments. Run sheldon --help for available commands.")
			return 2
		}
		var err error
		prURL, err = githubpr.NormalizeURL(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	if err := start(ctx, prURL, stdout, stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "Sheldon:", err)
		return 1
	}
	return 0
}

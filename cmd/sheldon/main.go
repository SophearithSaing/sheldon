package main

import (
	"fmt"
	"io"
	"os"
)

var version = "dev"

const usage = `Sheldon — Don't just merge it. Understand it.

Usage:
  sheldon --help
  sheldon --version

This is the application scaffold. The local server and PR opening are not implemented yet.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
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
	fmt.Fprintln(stderr, "Unsupported command or arguments. Run sheldon --help for the available scaffold commands.")
	return 2
}

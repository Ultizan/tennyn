package main

import (
	"fmt"
	"io"
	"os"
)

// version is overwritten at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "version" {
		fmt.Fprintf(stdout, "tennyn %s\n", version)
		return 0
	}
	fmt.Fprintln(stderr, "usage: tennyn [--config tennyn.yml] [--json] <check|why|coverage|stale|cheatsheet|version>")
	return 2
}

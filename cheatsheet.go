package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func (c *cli) cheatsheet(args []string) int {
	fs := flag.NewFlagSet("cheatsheet", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	check := fs.String("check", "", "verify FILE already contains the cheat sheet table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprint(c.stderr, usage)
		return 2
	}
	table := Cheatsheet(c.cfg)
	if *check == "" {
		fmt.Fprint(c.stdout, table)
		return 0
	}
	b, err := os.ReadFile(*check)
	if err != nil {
		fmt.Fprintf(c.stderr, "tennyn: cannot read %s: %v\n", *check, err)
		return 2
	}
	ok := containsBlock(string(b), table)
	if c.json {
		c.emit(struct {
			OK   bool   `json:"ok"`
			File string `json:"file"`
		}{ok, *check})
		return exitBool(ok)
	}
	if !ok {
		fmt.Fprintf(c.stdout, "tennyn: cheat sheet in %s is out of date — run `tennyn cheatsheet` and paste the table\n", *check)
	}
	return exitBool(ok)
}

// containsBlock reports whether needle appears in haystack as a contiguous
// run of lines, after normalizing CRLF to LF and trimming trailing
// whitespace from every line in both.
func containsBlock(haystack, needle string) bool {
	h := normalizedLines(haystack)
	n := normalizedLines(needle)
	for len(n) > 0 && n[len(n)-1] == "" {
		n = n[:len(n)-1]
	}
	if len(n) == 0 {
		return true
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func normalizedLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return lines
}

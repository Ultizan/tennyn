package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// version is overwritten at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `usage: tennyn [--config tennyn.yml] [--json] <command>

  check [--base REF] [--stdin]   gate: fired rules must be satisfied or waived (exit 1 otherwise)
  why [--base REF | --stdin] PATH...   which rules a change to PATH... (or a diff against REF) would fire
  coverage [--all]               files no rule watches, dead rules, broken require targets (exit 1 on broken)
  stale                          rules whose watched paths moved after their required paths (exit 1 if any)
  cheatsheet [--check FILE]      markdown table of every rule (or verify FILE already has it)
  version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type cli struct {
	cfg    *Config
	repo   repo
	json   bool
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("tennyn", flag.ContinueOnError)
	global.SetOutput(stderr)
	cfgPath := global.String("config", "tennyn.yml", "path to the rules file")
	asJSON := global.Bool("json", false, "machine-readable output")
	if err := global.Parse(args); err != nil {
		return 2
	}
	rest := global.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	c := &cli{json: *asJSON, stdin: stdin, stdout: stdout, stderr: stderr}
	cmd, sub := rest[0], rest[1:]
	if cmd == "version" {
		fmt.Fprintf(stdout, "tennyn %s\n", version)
		return 0
	}
	// Work from the repo root so patterns are root-anchored regardless of cwd.
	// Outside a repo (e.g. `why` on a loose config) we stay put.
	if root, err := (repo{dir: "."}).root(); err == nil {
		c.repo = repo{dir: root}
		if !strings.Contains(*cfgPath, "/") && !strings.Contains(*cfgPath, "\\") {
			if err := os.Chdir(root); err != nil {
				fmt.Fprintf(stderr, "tennyn: cannot enter repo root %s: %v\n", root, err)
				return 2
			}
		}
	} else {
		c.repo = repo{dir: "."}
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "tennyn: %v\n", err)
		return 2
	}
	c.cfg = cfg
	switch cmd {
	case "check":
		return c.check(sub)
	case "why":
		return c.why(sub)
	case "coverage":
		return c.coverage(sub)
	case "stale":
		if len(sub) > 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return c.stale()
	case "cheatsheet":
		return c.cheatsheet(sub)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func (c *cli) emit(v any) {
	enc := json.NewEncoder(c.stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// annotate mirrors a failure line as a CI annotation where the runner supports one.
func (c *cli) annotate(msg string) {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		fmt.Fprintf(c.stdout, "::error::%s\n", msg)
	case os.Getenv("TF_BUILD") == "True":
		fmt.Fprintf(c.stdout, "##vso[task.logissue type=error]%s\n", msg)
	}
}

func (c *cli) pathsFrom(useStdin bool, args []string) []string {
	if !useStdin {
		return args
	}
	b, _ := io.ReadAll(c.stdin)
	return splitLines(string(b))
}

func (c *cli) check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	base := fs.String("base", "", "compare against REF (default: detected from CI env)")
	useStdin := fs.Bool("stdin", false, "read changed paths from stdin instead of git")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var changed []string
	if *useStdin {
		changed = c.pathsFrom(true, nil)
	} else {
		ref := *base
		if ref == "" {
			ref = detectBase()
		}
		if ref == "" {
			fmt.Fprintln(c.stderr, "tennyn: no PR base found; pass --base REF (or --stdin), or run under GitHub/Forgejo/Azure DevOps PR events")
			return 2
		}
		var err error
		if changed, err = c.repo.changed(ref); err != nil {
			fmt.Fprintf(c.stderr, "tennyn: %v\n", err)
			return 2
		}
	}
	hits, ok := Evaluate(c.cfg, changed, labels())
	if c.json {
		c.emit(struct {
			OK    bool  `json:"ok"`
			Fired []Hit `json:"fired"`
		}{ok, orEmpty(hits)})
		return exitBool(ok)
	}
	for _, h := range hits {
		switch {
		case !h.Satisfied && h.Bypassed:
			fmt.Fprintf(c.stdout, "tennyn: rule %q waived by label %q\n", h.Name, h.Rule.Bypass)
		case !h.Satisfied:
			msg := fmt.Sprintf("rule %q fired by %d file(s) but none of its required paths changed", h.Name, len(h.Files))
			fmt.Fprintf(c.stdout, "tennyn: %s\n", msg)
			c.annotate(msg)
			if h.Rule.Why != "" {
				fmt.Fprintf(c.stdout, "  why: %s\n", h.Rule.Why)
			}
			fmt.Fprintf(c.stdout, "  changed: %s\n", strings.Join(h.Files, ", "))
			fmt.Fprintf(c.stdout, "  required (any of): %s\n", strings.Join(h.Rule.Require, ", "))
			if h.Rule.Bypass != "" {
				fmt.Fprintf(c.stdout, "  waive: apply PR label %q with a justification\n", h.Rule.Bypass)
			}
		}
	}
	if ok {
		fmt.Fprintf(c.stdout, "tennyn: ok (%d rule(s) fired, %d file(s) changed)\n", len(hits), len(changed))
	}
	return exitBool(ok)
}

func (c *cli) why(args []string) int {
	fs := flag.NewFlagSet("why", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	useStdin := fs.Bool("stdin", false, "read paths from stdin")
	base := fs.String("base", "", "compare against REF instead of naming paths")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	positional := fs.Args()
	if *base != "" && (*useStdin || len(positional) > 0) {
		fmt.Fprintln(c.stderr, "tennyn: --base is mutually exclusive with --stdin and PATH...")
		fmt.Fprint(c.stderr, usage)
		return 2
	}
	var paths []string
	if *base != "" {
		var err error
		if paths, err = c.repo.changed(*base); err != nil {
			fmt.Fprintf(c.stderr, "tennyn: %v\n", err)
			return 2
		}
	} else {
		paths = c.pathsFrom(*useStdin, positional)
	}
	hits := fired(c.cfg, paths)
	if c.json {
		rules := make([]Rule, 0, len(hits))
		for _, h := range hits {
			rules = append(rules, *h.Rule)
		}
		c.emit(rules)
		return 0
	}
	if len(hits) == 0 {
		fmt.Fprintln(c.stdout, "tennyn: no rules fire for these paths")
		return 0
	}
	for _, h := range hits {
		fmt.Fprintf(c.stdout, "rule %q (fired by %s)\n", h.Name, strings.Join(h.Files, ", "))
		fmt.Fprintf(c.stdout, "  update any of: %s\n", strings.Join(h.Rule.Require, ", "))
		if h.Rule.Why != "" {
			fmt.Fprintf(c.stdout, "  why: %s\n", h.Rule.Why)
		}
		if h.Rule.Owner != "" {
			fmt.Fprintf(c.stdout, "  owner: %s\n", h.Rule.Owner)
		}
		if h.Rule.Bypass != "" {
			fmt.Fprintf(c.stdout, "  waiver label: %s\n", h.Rule.Bypass)
		}
	}
	return 0
}

func (c *cli) coverage(args []string) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	all := fs.Bool("all", false, "list every uncovered file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tracked, err := c.repo.tracked()
	if err != nil {
		fmt.Fprintf(c.stderr, "tennyn: %v\n", err)
		return 2
	}
	rep := Coverage(c.cfg, tracked, *all)
	ok := len(rep.BrokenTargets) == 0
	if c.json {
		c.emit(rep)
		return exitBool(ok)
	}
	if rep.Ignored > 0 {
		fmt.Fprintf(c.stdout, "tennyn: %d of %d tracked files are watched by a rule (%d ignored)\n", rep.Covered, rep.Total, rep.Ignored)
	} else {
		fmt.Fprintf(c.stdout, "tennyn: %d of %d tracked files are watched by a rule\n", rep.Covered, rep.Total)
	}
	if len(rep.Uncovered) > 0 {
		fmt.Fprintln(c.stdout, "uncovered by top-level directory:")
		for _, dir := range sortedKeys(rep.Uncovered) {
			fmt.Fprintf(c.stdout, "  %-30s %d\n", dir, rep.Uncovered[dir])
		}
	}
	for _, f := range rep.UncoveredFiles {
		fmt.Fprintf(c.stdout, "  - %s\n", f)
	}
	for _, r := range rep.DeadRules {
		fmt.Fprintf(c.stdout, "dead rule (watches nothing tracked): %s\n", r)
	}
	for _, t := range rep.BrokenTargets {
		fmt.Fprintf(c.stdout, "broken target: rule %q requires %q but no tracked file matches\n", t.Rule, t.Pattern)
	}
	return exitBool(ok)
}

func (c *cli) stale() int {
	rows, err := Stale(c.cfg, c.repo)
	if err != nil {
		fmt.Fprintf(c.stderr, "tennyn: %v\n", err)
		return 2
	}
	anyStale := false
	for _, s := range rows {
		anyStale = anyStale || s.Stale
	}
	if c.json {
		c.emit(rows)
		return exitBool(!anyStale)
	}
	fmt.Fprintf(c.stdout, "%-20s %-12s %-12s %-20s %s\n", "rule", "when moved", "require moved", "status", "kept fresh by")
	for _, s := range rows {
		status := "fresh"
		if s.Stale {
			status = fmt.Sprintf("STALE by %d day(s)", s.LagDays)
		} else if s.WhenTS == 0 {
			status = "never touched"
		}
		fmt.Fprintf(c.stdout, "%-20s %-12s %-12s %-20s %s\n", s.Rule, day(s.WhenTS), day(s.RequireTS), status, s.FreshBy)
	}
	return exitBool(!anyStale)
}

func day(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

func exitBool(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func orEmpty(h []Hit) []Hit {
	if h == nil {
		return []Hit{}
	}
	return h
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

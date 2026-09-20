package main

import (
	"reflect"
	"testing"
)

func mustCfg(t *testing.T, src string) *Config {
	t.Helper()
	cfg, err := ParseConfig([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const gateYAML = `
rules:
  - name: docs
    when: [scripts/, Makefile]
    require: [docs/, README.md]
    bypass: docs-unaffected
  - name: ops
    when: [scripts/deploy/]
    require: [RUNBOOK.md]
  - name: security
    when: [auth/]
    require: [SECURITY.md]
    bypass: security-reviewed
  - name: api
    when: [daemon/*_handlers.go]
    require: [api/openapi.yaml]
`

func TestEvaluate(t *testing.T) {
	cfg := mustCfg(t, gateYAML)
	cases := []struct {
		name    string
		changed []string
		labels  []string
		wantOK  bool
		fired   []string // rule names in config order
	}{
		{"nothing fires", []string{"src/x.go"}, nil, true, nil},
		{"exact require satisfies", []string{"Makefile", "README.md"}, nil, true, []string{"docs"}},
		{"dir prefix require satisfies", []string{"scripts/a.sh", "docs/guide.md"}, nil, true, []string{"docs"}},
		{"unsatisfied fails", []string{"scripts/a.sh"}, nil, false, []string{"docs"}},
		{"require-only change does not fire", []string{"docs/guide.md"}, nil, true, nil},
		{"label waives only its rule", []string{"scripts/a.sh", "auth/x.go"}, []string{"docs-unaffected"}, false, []string{"docs", "security"}},
		{"both labels waive both", []string{"scripts/a.sh", "auth/x.go"}, []string{"docs-unaffected", "security-reviewed"}, true, []string{"docs", "security"}},
		{"rule without bypass ignores labels", []string{"daemon/t_handlers.go"}, []string{"docs-unaffected"}, false, []string{"api"}},
		{"glob require satisfies", []string{"daemon/t_handlers.go", "api/openapi.yaml"}, nil, true, []string{"api"}},
		{"multiple rules fire from one file", []string{"scripts/deploy/x.sh"}, nil, false, []string{"docs", "ops"}},
	}
	for _, c := range cases {
		hits, ok := Evaluate(cfg, c.changed, c.labels)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v want %v (%+v)", c.name, ok, c.wantOK, hits)
		}
		var names []string
		for _, h := range hits {
			names = append(names, h.Name)
		}
		if !reflect.DeepEqual(names, c.fired) {
			t.Errorf("%s: fired %v want %v", c.name, names, c.fired)
		}
	}
}

func TestEvaluateHitDetails(t *testing.T) {
	cfg := mustCfg(t, gateYAML)
	hits, _ := Evaluate(cfg, []string{"scripts/a.sh", "scripts/b.sh", "auth/x.go"}, []string{" docs-unaffected "})
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	d := hits[0]
	if !reflect.DeepEqual(d.Files, []string{"scripts/a.sh", "scripts/b.sh"}) || !d.Bypassed || d.Satisfied {
		t.Errorf("docs hit wrong: %+v", d)
	}
	if s := hits[1]; s.Bypassed || s.Satisfied || s.Rule.Name != "security" {
		t.Errorf("security hit wrong: %+v", s)
	}
}

func TestSplitLabels(t *testing.T) {
	got := splitLabels(" a, b ,,c ")
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %v", got)
	}
	if got := splitLabels(""); len(got) != 0 {
		t.Fatalf("empty input should give no labels, got %v", got)
	}
}

func TestCoverage(t *testing.T) {
	cfg := mustCfg(t, gateYAML)
	tracked := []string{"Makefile", "README.md", "scripts/a.sh", "src/main.go", "src/util.go", "docs/g.md", "LICENSE"}
	rep := Coverage(cfg, tracked, true)
	if rep.Total != 7 || rep.Covered != 2 {
		t.Fatalf("total/covered = %d/%d", rep.Total, rep.Covered)
	}
	if rep.Uncovered["src"] != 2 || rep.Uncovered["."] != 2 || rep.Uncovered["docs"] != 1 {
		t.Fatalf("uncovered = %v", rep.Uncovered)
	}
	if len(rep.UncoveredFiles) != 5 {
		t.Fatalf("uncovered files = %v", rep.UncoveredFiles)
	}
	if !reflect.DeepEqual(rep.DeadRules, []string{"ops", "security", "api"}) {
		t.Fatalf("dead = %v", rep.DeadRules)
	}
	want := []Target{{"ops", "RUNBOOK.md"}, {"security", "SECURITY.md"}, {"api", "api/openapi.yaml"}}
	if !reflect.DeepEqual(rep.BrokenTargets, want) {
		t.Fatalf("broken = %v", rep.BrokenTargets)
	}
	if rep2 := Coverage(cfg, tracked, false); rep2.UncoveredFiles != nil {
		t.Fatal("listAll=false must not list files")
	}
}

func TestCoverageIgnore(t *testing.T) {
	cfg := mustCfg(t, gateYAML)
	tracked := []string{"Makefile", "README.md", "scripts/a.sh", "src/main.go", "src/util.go", "docs/g.md", "LICENSE"}
	rep := Coverage(cfg, tracked, true)
	if rep.Ignored != 0 {
		t.Fatalf("no ignore: list must have zero ignored, got %d", rep.Ignored)
	}
	cfg2 := mustCfg(t, gateYAML+"ignore: [src/, LICENSE]\n")
	rep2 := Coverage(cfg2, tracked, true)
	if rep2.Ignored != 3 {
		t.Fatalf("ignored = %d, want 3", rep2.Ignored)
	}
	if rep2.Total != 4 {
		t.Fatalf("total must exclude ignored files, got %d", rep2.Total)
	}
	if rep2.Covered != 2 {
		t.Fatalf("covered = %d, want 2", rep2.Covered)
	}
	for _, f := range rep2.UncoveredFiles {
		if f == "src/main.go" || f == "src/util.go" || f == "LICENSE" {
			t.Fatalf("ignored file %q must not appear in uncovered_files: %v", f, rep2.UncoveredFiles)
		}
	}
	if rep2.Uncovered["src"] != 0 {
		t.Fatalf("ignored dir must not appear in uncovered map: %v", rep2.Uncovered)
	}
	// ignore must not hide broken targets or dead rules.
	if !reflect.DeepEqual(rep2.DeadRules, []string{"ops", "security", "api"}) {
		t.Fatalf("dead rules must be unaffected by ignore: %v", rep2.DeadRules)
	}
	want := []Target{{"ops", "RUNBOOK.md"}, {"security", "SECURITY.md"}, {"api", "api/openapi.yaml"}}
	if !reflect.DeepEqual(rep2.BrokenTargets, want) {
		t.Fatalf("broken targets must be unaffected by ignore: %v", rep2.BrokenTargets)
	}
}

func TestCheatsheet(t *testing.T) {
	cfg := mustCfg(t, `
rules:
  - name: docs
    when: [scripts/, Makefile]
    require: [docs/, README.md]
    why: scripts are contract-bearing
    owner: operator
    bypass: docs-unaffected
`)
	got := Cheatsheet(cfg)
	want := "| Rule | If you touch | Then update (any of) | Why | Owner | Waiver label |\n" +
		"|---|---|---|---|---|---|\n" +
		"| docs | `scripts/`, `Makefile` | `docs/`, `README.md` | scripts are contract-bearing | operator | `docs-unaffected` |\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

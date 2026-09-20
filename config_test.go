package main

import (
	"strings"
	"testing"
)

func TestPatternForms(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"docs/", "docs/a.md", true},
		{"docs/", "docs/sub/b.md", true},
		{"docs/", "docsx/a.md", false},
		{"docs/", "sub/docs/a.md", false},
		{"README.md", "README.md", true},
		{"README.md", "sub/README.md", false},
		{"**/CHANGELOG.md", "CHANGELOG.md", true},
		{"**/CHANGELOG.md", "a/b/CHANGELOG.md", true},
		{"daemon/*_handlers.go", "daemon/task_handlers.go", true},
		{"daemon/*_handlers.go", "daemon/sub/task_handlers.go", false},
		{"lib/{auth,rbac}/**", "lib/rbac/x.ex", true},
		{"lib/{auth,rbac}/**", "lib/other/x.ex", false},
		{"env/RUNBOOK-*.md", "env/RUNBOOK-deploy.md", true},
	}
	for _, c := range cases {
		p, err := compilePattern(c.pat)
		if err != nil {
			t.Fatalf("%q: %v", c.pat, err)
		}
		if got := p.Match(c.path); got != c.want {
			t.Errorf("%q vs %q: got %v want %v", c.pat, c.path, got, c.want)
		}
	}
	if _, err := compilePattern("bad[pattern"); err == nil {
		t.Error("expected error for unbalanced bracket")
	}
}

const goodYAML = `
anchors:
  docs: &docs [docs/, AGENTS.md]
rules:
  - name: docs
    when: [scripts/, Makefile]
    require: *docs
    why: scripts are contract-bearing
    owner: operator
    bypass: docs-unaffected
  - name: env
    when: [environments/local-rancher/]
    require: [environments/local-rancher/RUNBOOK-*.md, *docs]
`

func TestParseConfigFlattensAnchors(t *testing.T) {
	cfg, err := ParseConfig([]byte(goodYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("want 2 rules, got %d", len(cfg.Rules))
	}
	env := cfg.Rules[1]
	want := []string{"environments/local-rancher/RUNBOOK-*.md", "docs/", "AGENTS.md"}
	if strings.Join(env.Require, ",") != strings.Join(want, ",") {
		t.Fatalf("flattened require = %v, want %v", env.Require, want)
	}
	if !matchAny(env.require, "docs/x.md") || !matchAny(env.when, "environments/local-rancher/a.yaml") {
		t.Fatal("compiled patterns missing")
	}
	if cfg.Rules[0].Bypass != "docs-unaffected" || cfg.Rules[0].Owner != "operator" {
		t.Fatal("scalar fields not decoded")
	}
}

func TestParseConfigIgnore(t *testing.T) {
	cfg := mustCfg(t, "ignore: [vendor/, '*.gen.go']\nrules:\n  - {name: x, when: [a], require: [b]}\n")
	if !matchAny(cfg.ignore, "vendor/x.go") || !matchAny(cfg.ignore, "y.gen.go") {
		t.Fatalf("ignore patterns not compiled: %+v", cfg.ignore)
	}
	cfg2 := mustCfg(t, "rules:\n  - {name: x, when: [a], require: [b]}\n")
	if len(cfg2.ignore) != 0 {
		t.Fatalf("absent ignore must compile to empty, got %v", cfg2.ignore)
	}
}

func TestParseConfigRejects(t *testing.T) {
	bad := map[string]string{
		"no rules":        `rules: []`,
		"empty":           ``,
		"missing name":    "rules:\n  - when: [a]\n    require: [b]\n",
		"duplicate name":  "rules:\n  - {name: x, when: [a], require: [b]}\n  - {name: x, when: [a], require: [b]}\n",
		"empty when":      "rules:\n  - {name: x, when: [], require: [b]}\n",
		"empty require":   "rules:\n  - {name: x, when: [a]}\n",
		"unknown key":     "rules:\n  - {name: x, when: [a], require: [b], owner2: y}\n",
		"invalid pattern": "rules:\n  - {name: x, when: ['a[b'], require: [b]}\n",
		"non-string item": "rules:\n  - {name: x, when: [{k: v}], require: [b]}\n",
		"invalid ignore":  "ignore: ['a[b']\nrules:\n  - {name: x, when: [a], require: [b]}\n",
	}
	for label, src := range bad {
		if _, err := ParseConfig([]byte(src)); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

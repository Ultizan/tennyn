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

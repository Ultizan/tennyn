package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decisionFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "decision", "valid.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCompileDecisionGoldenAndOrdering(t *testing.T) {
	src := decisionFixture(t)
	cfg := mustCfg(t, src)
	got, err := CompileDecision(cfg, "choose")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "decision", "canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.TrimSpace(want)) {
		t.Fatalf("canonical mismatch\n got: %s\nwant: %s", got, want)
	}

	// YAML mapping order is not semantic: canonical struct order fixes output.
	reordered := strings.Replace(src,
		"      version: 1\n      baseline: incumbent-v1\n      utility: verified-progress-v1",
		"      utility: verified-progress-v1\n      baseline: incumbent-v1\n      version: 1", 1)
	reordered = strings.Replace(reordered,
		"        candidates: 16\n        features: 8\n        selected: 1",
		"        selected: 1\n        features: 8\n        candidates: 16", 1)
	reorderedOut, err := CompileDecision(mustCfg(t, reordered), "choose")
	if err != nil || !bytes.Equal(got, reorderedOut) {
		t.Fatalf("map reordering changed identity: %v\n%s", err, reorderedOut)
	}

	// Array order is semantic and therefore changes the compiled contract.
	actions := strings.Replace(src, "actions: [read_context, run_tests]", "actions: [run_tests, read_context]", 1)

	actionOut, err := CompileDecision(mustCfg(t, actions), "choose")
	if err != nil || bytes.Equal(got, actionOut) {
		t.Fatalf("action axis reordering did not change identity: %v", err)
	}
	features := strings.Replace(src, "features: [progress]", "features: [progress, reliability]", 1)
	features = strings.Replace(features, "theta_q: [10000]", "theta_q: [10000, -2500]", 1)
	features = strings.Replace(features, "weights_q: [[10000], [10000]]", "weights_q: [[10000, 500], [7000, 2500]]", 1)
	features = strings.Replace(features, "features: [progress, reliability]", "features: [reliability, progress]", 1)
	featureOut, err := CompileDecision(mustCfg(t, features), "choose")
	if err != nil || bytes.Equal(got, featureOut) {
		t.Fatalf("feature axis reordering did not change identity: %v", err)
	}
}

func TestDecisionEscapingMatchesGoJSON(t *testing.T) {
	src := strings.Replace(decisionFixture(t), "baseline: incumbent-v1", "baseline: 'é<&>  '", 1)
	out, err := CompileDecision(mustCfg(t, src), "choose")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("é\\u003c\\u0026\\u003e\\u2028\\u2029")) {
		t.Fatalf("Go JSON escaping changed: %s", out)
	}
}

func TestDecisionKernelRejectsInvalidYAML(t *testing.T) {
	src := decisionFixture(t)
	cases := []struct{ name, old, replacement string }{
		{"unknown kernel key", "      version: 1", "      version: 1\n      extra: true"},
		{"duplicate kernel key", "      version: 1", "      version: 1\n      version: 1"},
		{"alias", "      baseline: incumbent-v1", "      baseline: &base incumbent-v1\n      utility: *base"},
		{"duplicate features", "features: [progress]", "features: [progress, progress]"},
		{"fractional integer", "theta_q: [10000]", "theta_q: [1.0]"},
		{"oversized integer", "horizon_steps: 4", "horizon_steps: 9223372036854775808"},
		{"negative limit", "evaluator_calls: 3", "evaluator_calls: -1"},
		{"selected count", "selected: 1", "selected: 2"},
		{"feature capacity", "features: 8", "features: 0"},
		{"theta axis", "theta_q: [10000]", "theta_q: []"},
		{"weight action axis", "weights_q: [[10000], [10000]]", "weights_q: [[10000]]"},
		{"weight feature axis", "weights_q: [[10000], [10000]]", "weights_q: [[10000, 500], [7000, 2500]]"},
		{"duplicate action", "actions: [read_context, run_tests]", "actions: [read_context, read_context]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Replace(src, tc.old, tc.replacement, 1)
			if bad == src {
				t.Fatal("test mutation did not apply")
			}
			if _, err := ParseConfig([]byte(bad)); err == nil {
				t.Fatal("expected invalid kernel to be rejected")
			}
		})
	}
	t.Run("duplicate rule kernel property", func(t *testing.T) {
		second := `    kernel: {version: 1, baseline: incumbent-v1, utility: verified-progress-v1, horizon_steps: 4, features: [progress], actions: [read_context, run_tests], theta_q: [10000], weights_q: [[10000], [10000]], limits: {candidates: 16, features: 8, selected: 1, evaluator_calls: 3, evaluator_parallel: 2, evaluator_tokens: 768, decision_timeout_ms: 5000, think_repeats: 1}}
`
		bad := strings.Replace(src, "        think_repeats: 1\n", "        think_repeats: 1\n"+second, 1)
		if _, err := ParseConfig([]byte(bad)); err == nil {
			t.Fatal("expected duplicate rule kernel property to be rejected")
		}
	})
	t.Run("extra document", func(t *testing.T) {
		if _, err := ParseConfig([]byte(src + "\n---\nrules: []\n")); err == nil {
			t.Fatal("expected second document to be rejected")
		}
	})
	t.Run("limits mapping duplicate", func(t *testing.T) {
		bad := strings.Replace(src, "        candidates: 16", "        candidates: 16\n        candidates: 4", 1)
		if _, err := ParseConfig([]byte(bad)); err == nil {
			t.Fatal("expected duplicate limit to be rejected")
		}
	})
}

func TestKernelRejectsWholeBlockAliasAndNull(t *testing.T) {
	src := decisionFixture(t)
	t.Run("whole block alias", func(t *testing.T) {
		parts := strings.SplitN(src, "    kernel:\n", 2)
		if len(parts) != 2 {
			t.Fatal("kernel block missing from fixture")
		}
		lines := strings.Split(strings.TrimSuffix(parts[1], "\n"), "\n")
		for i := range lines {
			lines[i] = strings.TrimPrefix(lines[i], "  ")
		}
		aliasConfig := "anchors:\n  shared: &anchor\n" + strings.Join(lines, "\n") + "\n" + parts[0] + "    kernel: *anchor\n"
		if _, err := ParseConfig([]byte(aliasConfig)); err == nil {
			t.Fatal("expected whole-kernel alias to be rejected")
		}
	})
	t.Run("explicit null", func(t *testing.T) {
		nullConfig := strings.Replace(src, "    kernel:\n", "    kernel: null\n", 1)
		if _, err := ParseConfig([]byte(nullConfig)); err == nil {
			t.Fatal("expected explicit null kernel to be rejected")
		}
	})
}

func TestDecisionAllowsExhaustedZeroLimits(t *testing.T) {
	src := decisionFixture(t)
	for _, old := range []string{"evaluator_calls: 3", "evaluator_parallel: 2", "evaluator_tokens: 768", "decision_timeout_ms: 5000", "think_repeats: 1"} {
		src = strings.Replace(src, old, strings.Split(old, ":")[0]+": 0", 1)
	}
	if _, err := ParseConfig([]byte(src)); err != nil {
		t.Fatalf("zero nonnegative limits should be accepted: %v", err)
	}
}

func TestCompileDecisionRequiresUniqueKernelRule(t *testing.T) {
	cfg := mustCfg(t, decisionFixture(t))
	if _, err := CompileDecision(cfg, "missing"); err == nil {
		t.Fatal("expected missing rule error")
	}
	cfg.Rules = append(cfg.Rules, cfg.Rules[0])
	if _, err := CompileDecision(cfg, "choose"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	cfg.Rules = cfg.Rules[:1]
	cfg.Rules[0].Kernel = nil
	if _, err := CompileDecision(cfg, "choose"); err == nil || !strings.Contains(err.Error(), "no kernel") {
		t.Fatalf("expected missing kernel error, got %v", err)
	}
}

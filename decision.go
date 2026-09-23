package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// DecisionContract is the version-1, declarative scoring contract carried by a rule.
type DecisionContract struct {
	Version      int64          `yaml:"version" json:"version"`
	Baseline     string         `yaml:"baseline" json:"baseline"`
	Utility      string         `yaml:"utility" json:"utility"`
	HorizonSteps int64          `yaml:"horizon_steps" json:"horizon_steps"`
	Features     []string       `yaml:"features" json:"features"`
	Actions      []string       `yaml:"actions" json:"actions"`
	ThetaQ       []int64        `yaml:"theta_q" json:"theta_q"`
	WeightsQ     [][]int64      `yaml:"weights_q" json:"weights_q"`
	Limits       DecisionLimits `yaml:"limits" json:"limits"`
	Digest       string         `yaml:"-" json:"digest"`
}

type DecisionLimits struct {
	Candidates        int64 `yaml:"candidates" json:"candidates"`
	Features          int64 `yaml:"features" json:"features"`
	Selected          int64 `yaml:"selected" json:"selected"`
	EvaluatorCalls    int64 `yaml:"evaluator_calls" json:"evaluator_calls"`
	EvaluatorParallel int64 `yaml:"evaluator_parallel" json:"evaluator_parallel"`
	EvaluatorTokens   int64 `yaml:"evaluator_tokens" json:"evaluator_tokens"`
	DecisionTimeoutMS int64 `yaml:"decision_timeout_ms" json:"decision_timeout_ms"`
	ThinkRepeats      int64 `yaml:"think_repeats" json:"think_repeats"`
}

type decisionCanonical struct {
	Version      int64          `json:"version"`
	Baseline     string         `json:"baseline"`
	Utility      string         `json:"utility"`
	HorizonSteps int64          `json:"horizon_steps"`
	Features     []string       `json:"features"`
	Actions      []string       `json:"actions"`
	ThetaQ       []int64        `json:"theta_q"`
	WeightsQ     [][]int64      `json:"weights_q"`
	Limits       DecisionLimits `json:"limits"`
}

func (c *DecisionContract) UnmarshalYAML(n *yaml.Node) error {
	v, err := parseDecisionContract(n)
	if err == nil {
		*c = v
	}
	return err
}

func rejectDecisionAliases(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: YAML aliases are not allowed in kernel", n.Line)
	}
	for _, child := range n.Content {
		if err := rejectDecisionAliases(child); err != nil {
			return err
		}
	}
	return nil
}

func decisionFields(n *yaml.Node, required []string) (map[string]*yaml.Node, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected mapping", n.Line)
	}
	want := make(map[string]bool, len(required))
	for _, k := range required {
		want[k] = true
	}
	got := make(map[string]*yaml.Node, len(required))
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, fmt.Errorf("line %d: expected string key", key.Line)
		}
		if _, ok := got[key.Value]; ok {
			return nil, fmt.Errorf("line %d: duplicate kernel key %q", key.Line, key.Value)
		}
		if !want[key.Value] {
			return nil, fmt.Errorf("line %d: unknown kernel key %q", key.Line, key.Value)
		}
		got[key.Value] = n.Content[i+1]
	}
	for _, k := range required {
		if _, ok := got[k]; !ok {
			return nil, fmt.Errorf("line %d: missing kernel key %q", n.Line, k)
		}
	}
	return got, nil
}

func decisionString(n *yaml.Node, name string) (string, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", fmt.Errorf("line %d: %s must be a string", n.Line, name)
	}
	return n.Value, nil
}

func decisionInt(n *yaml.Node, name string) (int64, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" {
		return 0, fmt.Errorf("line %d: %s must be a decimal integer", n.Line, name)
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("line %d: %s is outside signed 64-bit integer range", n.Line, name)
	}
	return v, nil
}

func decisionStringList(n *yaml.Node, name string) ([]string, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: %s must be a list", n.Line, name)
	}
	out := make([]string, len(n.Content))
	seen := make(map[string]bool, len(out))
	for i, item := range n.Content {
		v, err := decisionString(item, name)
		if err != nil {
			return nil, err
		}
		if v == "" {
			return nil, fmt.Errorf("%s entries must be nonempty", name)
		}
		if seen[v] {
			return nil, fmt.Errorf("duplicate %s entry %q", name, v)
		}
		seen[v] = true
		out[i] = v
	}
	return out, nil
}

func decisionIntList(n *yaml.Node, name string, low, high int64) ([]int64, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: %s must be a list", n.Line, name)
	}
	out := make([]int64, len(n.Content))
	for i, item := range n.Content {
		v, err := decisionInt(item, name)
		if err != nil {
			return nil, err
		}
		if v < low || v > high {
			return nil, fmt.Errorf("%s entry %d outside [%d,%d]", name, i, low, high)
		}
		out[i] = v
	}
	return out, nil
}

func parseDecisionContract(n *yaml.Node) (DecisionContract, error) {
	if err := rejectDecisionAliases(n); err != nil {
		return DecisionContract{}, err
	}
	f, err := decisionFields(n, []string{"version", "baseline", "utility", "horizon_steps", "features", "actions", "theta_q", "weights_q", "limits"})
	if err != nil {
		return DecisionContract{}, err
	}
	var c DecisionContract
	if c.Version, err = decisionInt(f["version"], "version"); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, fmt.Errorf("kernel version must be 1")
	}
	if c.Baseline, err = decisionString(f["baseline"], "baseline"); err != nil {
		return c, err
	}
	if c.Utility, err = decisionString(f["utility"], "utility"); err != nil {
		return c, err
	}
	if c.Baseline == "" || c.Utility == "" {
		return c, fmt.Errorf("baseline and utility must be nonempty")
	}
	if c.HorizonSteps, err = decisionInt(f["horizon_steps"], "horizon_steps"); err != nil {
		return c, err
	}
	if c.HorizonSteps <= 0 {
		return c, fmt.Errorf("horizon_steps must be positive")
	}
	if c.Features, err = decisionStringList(f["features"], "features"); err != nil {
		return c, err
	}
	if len(c.Features) == 0 || len(c.Features) > 8 {
		return c, fmt.Errorf("features must contain 1 to 8 entries")
	}
	if c.Actions, err = decisionStringList(f["actions"], "actions"); err != nil {
		return c, err
	}
	if len(c.Actions) == 0 || len(c.Actions) > 16 {
		return c, fmt.Errorf("actions must contain 1 to 16 entries")
	}
	if c.ThetaQ, err = decisionIntList(f["theta_q"], "theta_q", -10000, 10000); err != nil {
		return c, err
	}
	if len(c.ThetaQ) != len(c.Features) {
		return c, fmt.Errorf("theta_q axis must match features")
	}
	weights := f["weights_q"]
	if weights.Kind != yaml.SequenceNode {
		return c, fmt.Errorf("weights_q must be a list of rows")
	}
	if len(weights.Content) != len(c.Actions) {
		return c, fmt.Errorf("weights_q rows must match actions")
	}
	c.WeightsQ = make([][]int64, len(weights.Content))
	for i, row := range weights.Content {
		c.WeightsQ[i], err = decisionIntList(row, "weights_q", 0, 10000)
		if err != nil {
			return c, err
		}
		if len(c.WeightsQ[i]) != len(c.Features) {
			return c, fmt.Errorf("weights_q row %d axis must match features", i)
		}
	}
	lf, err := decisionFields(f["limits"], []string{"candidates", "features", "selected", "evaluator_calls", "evaluator_parallel", "evaluator_tokens", "decision_timeout_ms", "think_repeats"})
	if err != nil {
		return c, fmt.Errorf("limits: %w", err)
	}
	vals := []*int64{&c.Limits.Candidates, &c.Limits.Features, &c.Limits.Selected, &c.Limits.EvaluatorCalls, &c.Limits.EvaluatorParallel, &c.Limits.EvaluatorTokens, &c.Limits.DecisionTimeoutMS, &c.Limits.ThinkRepeats}
	names := []string{"candidates", "features", "selected", "evaluator_calls", "evaluator_parallel", "evaluator_tokens", "decision_timeout_ms", "think_repeats"}
	for i, name := range names {
		*vals[i], err = decisionInt(lf[name], name)
		if err != nil {
			return c, err
		}
		if *vals[i] < 0 {
			return c, fmt.Errorf("limits.%s must be nonnegative", name)
		}
	}
	if c.Limits.Candidates < 1 || c.Limits.Candidates > 16 {
		return c, fmt.Errorf("limits.candidates must be 1 to 16")
	}
	if c.Limits.Features < 1 || c.Limits.Features > 8 || int64(len(c.Features)) > c.Limits.Features {
		return c, fmt.Errorf("limits.features must be a capacity from actual feature count to 8")
	}
	if c.Limits.Selected != 1 {
		return c, fmt.Errorf("version 1 requires limits.selected=1")
	}
	return c, nil
}

// CompileDecision returns canonical version-1 JSON, including its SHA-256 digest.
func CompileDecision(cfg *Config, rule string) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	var found *Rule
	for i := range cfg.Rules {
		if cfg.Rules[i].Name == rule {
			if found != nil {
				return nil, fmt.Errorf("rule %q is ambiguous", rule)
			}
			found = &cfg.Rules[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("rule %q not found", rule)
	}
	if found.Kernel == nil {
		return nil, fmt.Errorf("rule %q has no kernel contract", rule)
	}
	c := *found.Kernel
	input := decisionCanonical{c.Version, c.Baseline, c.Utility, c.HorizonSteps, c.Features, c.Actions, c.ThetaQ, c.WeightsQ, c.Limits}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	c.Digest = hex.EncodeToString(sum[:])
	return json.Marshal(c)
}

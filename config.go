package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Pattern is one when/require entry, anchored at the repo root.
// A trailing slash means "everything under this directory".
type Pattern struct {
	Raw  string
	glob string
}

func compilePattern(raw string) (Pattern, error) {
	g := raw
	if strings.HasSuffix(g, "/") {
		g += "**"
	}
	if g == "" || !doublestar.ValidatePattern(g) {
		return Pattern{}, fmt.Errorf("invalid pattern %q", raw)
	}
	return Pattern{Raw: raw, glob: g}, nil
}

func (p Pattern) Match(path string) bool {
	ok, _ := doublestar.Match(p.glob, path)
	return ok
}

func matchAny(pats []Pattern, path string) bool {
	for _, p := range pats {
		if p.Match(path) {
			return true
		}
	}
	return false
}

// patternList accepts a string, a list, or nested lists/aliases and flattens
// them, so `require: [a, *shared]` works despite YAML having no list merge.
type patternList []string

func (l *patternList) UnmarshalYAML(n *yaml.Node) error {
	var out []string
	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		switch n.Kind {
		case yaml.SequenceNode:
			for _, c := range n.Content {
				if err := walk(c); err != nil {
					return err
				}
			}
		case yaml.AliasNode:
			return walk(n.Alias)
		case yaml.ScalarNode:
			out = append(out, n.Value)
		default:
			return fmt.Errorf("line %d: expected a path pattern or list of them", n.Line)
		}
		return nil
	}
	if err := walk(n); err != nil {
		return err
	}
	*l = out
	return nil
}

type Rule struct {
	Name    string      `yaml:"name" json:"rule"`
	When    patternList `yaml:"when" json:"when"`
	Require patternList `yaml:"require" json:"require"`
	Why     string      `yaml:"why,omitempty" json:"why,omitempty"`
	Owner   string      `yaml:"owner,omitempty" json:"owner,omitempty"`
	Bypass  string      `yaml:"bypass,omitempty" json:"bypass,omitempty"`
	when    []Pattern
	require []Pattern
}

type Config struct {
	// Anchors is scratch space for YAML anchor definitions; its content is ignored.
	Anchors any         `yaml:"anchors,omitempty" json:"-"`
	Ignore  patternList `yaml:"ignore,omitempty" json:"ignore,omitempty"`
	Rules   []Rule      `yaml:"rules" json:"rules"`
	ignore  []Pattern
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := ParseConfig(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func ParseConfig(b []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(cfg.Rules) == 0 {
		return nil, errors.New("no rules defined")
	}
	var err error
	if cfg.ignore, err = compileAll(cfg.Ignore); err != nil {
		return nil, fmt.Errorf("ignore: %w", err)
	}
	seen := map[string]bool{}
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		switch {
		case r.Name == "":
			return nil, fmt.Errorf("rule %d: missing name", i+1)
		case seen[r.Name]:
			return nil, fmt.Errorf("rule %q: duplicate name", r.Name)
		case len(r.When) == 0:
			return nil, fmt.Errorf("rule %q: 'when' is empty", r.Name)
		case len(r.Require) == 0:
			return nil, fmt.Errorf("rule %q: 'require' is empty", r.Name)
		}
		seen[r.Name] = true
		var err error
		if r.when, err = compileAll(r.When); err != nil {
			return nil, fmt.Errorf("rule %q when: %w", r.Name, err)
		}
		if r.require, err = compileAll(r.Require); err != nil {
			return nil, fmt.Errorf("rule %q require: %w", r.Name, err)
		}
	}
	return &cfg, nil
}

func compileAll(raws []string) ([]Pattern, error) {
	out := make([]Pattern, 0, len(raws))
	for _, raw := range raws {
		p, err := compilePattern(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

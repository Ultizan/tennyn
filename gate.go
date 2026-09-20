package main

import "strings"

// Hit is one rule that fired for a change set.
type Hit struct {
	Rule      *Rule    `json:"-"`
	Name      string   `json:"rule"`
	Files     []string `json:"files"`
	Satisfied bool     `json:"satisfied"`
	Bypassed  bool     `json:"bypassed"`
}

// fired returns, in config order, every rule whose `when` matches a changed file.
func fired(cfg *Config, changed []string) []Hit {
	var hits []Hit
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		var files []string
		for _, f := range changed {
			if matchAny(r.when, f) {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			hits = append(hits, Hit{Rule: r, Name: r.Name, Files: files})
		}
	}
	return hits
}

// Evaluate runs the gate. ok is false when a fired rule is neither satisfied
// (a required path also changed) nor bypassed (its label is present).
func Evaluate(cfg *Config, changed, labels []string) (hits []Hit, ok bool) {
	ok = true
	has := map[string]bool{}
	for _, l := range labels {
		has[strings.TrimSpace(l)] = true
	}
	hits = fired(cfg, changed)
	for i := range hits {
		h := &hits[i]
		for _, f := range changed {
			if matchAny(h.Rule.require, f) {
				h.Satisfied = true
				break
			}
		}
		h.Bypassed = h.Rule.Bypass != "" && has[h.Rule.Bypass]
		if !h.Satisfied && !h.Bypassed {
			ok = false
		}
	}
	return hits, ok
}

func splitLabels(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

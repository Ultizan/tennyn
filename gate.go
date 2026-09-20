package main

import (
	"fmt"
	"strings"
)

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

// --- coverage ---

type Target struct {
	Rule    string `json:"rule"`
	Pattern string `json:"pattern"`
}

type CoverageReport struct {
	Total          int            `json:"total"`
	Covered        int            `json:"covered"`
	Ignored        int            `json:"ignored"`
	Uncovered      map[string]int `json:"uncovered"` // top-level dir ("." for root files) -> count
	UncoveredFiles []string       `json:"uncovered_files,omitempty"`
	DeadRules      []string       `json:"dead_rules"`
	BrokenTargets  []Target       `json:"broken_targets"`
}

// Coverage reports tracked files no rule watches, rules that watch nothing,
// and require targets that match no tracked file. Files matching the config's
// top-level `ignore` list are excluded from Total/Uncovered and counted in
// Ignored instead; ignore has no effect on DeadRules or BrokenTargets, which
// are computed against every tracked file regardless.
func Coverage(cfg *Config, tracked []string, listAll bool) CoverageReport {
	rep := CoverageReport{Uncovered: map[string]int{}, DeadRules: []string{}, BrokenTargets: []Target{}}
	for _, f := range tracked {
		if matchAny(cfg.ignore, f) {
			rep.Ignored++
			continue
		}
		rep.Total++
		covered := false
		for i := range cfg.Rules {
			if matchAny(cfg.Rules[i].when, f) {
				covered = true
				break
			}
		}
		if covered {
			rep.Covered++
			continue
		}
		rep.Uncovered[topDir(f)]++
		if listAll {
			rep.UncoveredFiles = append(rep.UncoveredFiles, f)
		}
	}
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if !anyTracked(r.when, tracked) {
			rep.DeadRules = append(rep.DeadRules, r.Name)
		}
		for _, p := range r.require {
			if !anyTracked([]Pattern{p}, tracked) {
				rep.BrokenTargets = append(rep.BrokenTargets, Target{r.Name, p.Raw})
			}
		}
	}
	return rep
}

func anyTracked(pats []Pattern, tracked []string) bool {
	for _, f := range tracked {
		if matchAny(pats, f) {
			return true
		}
	}
	return false
}

func topDir(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return "."
}

// --- stale ---

type Staleness struct {
	Rule      string `json:"rule"`
	WhenTS    int64  `json:"when_ts"`
	RequireTS int64  `json:"require_ts"`
	Stale     bool   `json:"stale"`
	LagDays   int    `json:"lag_days"`
	// FreshBy names what kept a non-stale, ever-touched rule fresh: the raw
	// `require` pattern whose newest commit is the newest among the rule's
	// require patterns, or "verified: <file>" when a verified: header
	// supplied the winning timestamp. Empty when the rule is stale or its
	// `when` paths have never been touched.
	FreshBy string `json:"fresh_by"`
}

// Stale compares, per rule, the newest commit touching `when` against the
// newest commit touching any single `require` pattern (or a newer
// `verified:` header in a require file). A rule is stale when its watched
// paths moved at least a full day (86400s) after its required paths last
// did; a same-day lag is fresh. Require patterns are timed individually
// (rather than as one combined group) so a fresh rule can report which
// pattern kept it fresh.
func Stale(cfg *Config, r repo) ([]Staleness, error) {
	// groups: for each rule, one `when` group followed by one group per
	// `require` pattern, in the same order as Rule.Require.
	groups := make([][]Pattern, 0, 2*len(cfg.Rules))
	whenIdx := make([]int, len(cfg.Rules))
	reqIdx := make([][]int, len(cfg.Rules))
	for i := range cfg.Rules {
		whenIdx[i] = len(groups)
		groups = append(groups, cfg.Rules[i].when)
		for _, p := range cfg.Rules[i].require {
			reqIdx[i] = append(reqIdx[i], len(groups))
			groups = append(groups, []Pattern{p})
		}
	}
	ts, err := r.newest(groups)
	if err != nil {
		return nil, err
	}
	tracked, err := r.tracked()
	if err != nil {
		return nil, err
	}
	out := make([]Staleness, len(cfg.Rules))
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		s := Staleness{Rule: rule.Name, WhenTS: ts[whenIdx[i]]}
		freshBy := ""
		for j, idx := range reqIdx[i] {
			if ts[idx] > s.RequireTS {
				s.RequireTS = ts[idx]
				freshBy = rule.Require[j]
			}
		}
		var reqFiles []string
		for _, f := range tracked {
			if matchAny(rule.require, f) {
				reqFiles = append(reqFiles, f)
			}
		}
		if v, vf := r.verifiedDateFile(reqFiles); v > s.RequireTS {
			s.RequireTS = v
			freshBy = "verified: " + vf
		}
		if s.WhenTS-s.RequireTS >= 86400 {
			s.Stale = true
			s.LagDays = int((s.WhenTS - s.RequireTS) / 86400)
		}
		if !s.Stale && s.WhenTS > 0 {
			s.FreshBy = freshBy
		}
		out[i] = s
	}
	return out, nil
}

// --- cheatsheet ---

func Cheatsheet(cfg *Config) string {
	var b strings.Builder
	b.WriteString("| Rule | If you touch | Then update (any of) | Why | Owner | Waiver label |\n|---|---|---|---|---|---|\n")
	for _, r := range cfg.Rules {
		waiver := ""
		if r.Bypass != "" {
			waiver = "`" + r.Bypass + "`"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			r.Name, codeList(r.When), codeList(r.Require), r.Why, r.Owner, waiver)
	}
	return b.String()
}

func codeList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "`" + s + "`"
	}
	return strings.Join(quoted, ", ")
}

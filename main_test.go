package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "tennyn ") {
		t.Fatalf("exit %d out %q err %q", code, out.String(), errb.String())
	}
}

// cliRepo builds a repo with a config and returns it; cwd is switched to it.
func cliRepo(t *testing.T) repo {
	t.Helper()
	r := newTestRepo(t)
	r.commit(t, 1000, map[string]string{
		"tennyn.yml": gateYAML,
		"README.md":  "hello",
		"scripts/a.sh": "x",
	})
	wd, _ := os.Getwd()
	if err := os.Chdir(r.dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	t.Setenv("TENNYN_LABELS", "")
	t.Setenv("GITHUB_BASE_REF", "")
	t.Setenv("SYSTEM_PULLREQUEST_TARGETBRANCH", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("TF_BUILD", "")
	return r
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestCheckStdin(t *testing.T) {
	cliRepo(t)
	code, out, _ := runCLI(t, "scripts/a.sh\n", "check", "--stdin")
	if code != 1 || !strings.Contains(out, `rule "docs"`) || !strings.Contains(out, "docs-unaffected") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, _ = runCLI(t, "scripts/a.sh\nREADME.md\n", "check", "--stdin")
	if code != 0 || !strings.Contains(out, "ok") {
		t.Fatalf("code %d out %q", code, out)
	}
	t.Setenv("TENNYN_LABELS", "docs-unaffected")
	code, out, _ = runCLI(t, "scripts/a.sh\n", "check", "--stdin")
	if code != 0 || !strings.Contains(out, "waived by label") {
		t.Fatalf("bypass: code %d out %q", code, out)
	}
	// Satisfied AND labeled must not print "waived": the label was unneeded.
	code, out, _ = runCLI(t, "scripts/a.sh\nREADME.md\n", "check", "--stdin")
	if code != 0 || !strings.Contains(out, "ok") || strings.Contains(out, "waived") {
		t.Fatalf("satisfied+labeled: code %d out %q", code, out)
	}
}

func TestCheckBaseAndAnnotations(t *testing.T) {
	r := cliRepo(t)
	r.git("branch", "base")
	r.commit(t, 2000, map[string]string{"scripts/b.sh": "y"})
	t.Setenv("GITHUB_ACTIONS", "true")
	code, out, _ := runCLI(t, "", "check", "--base", "base")
	if code != 1 || !strings.Contains(out, "::error::") {
		t.Fatalf("code %d out %q", code, out)
	}
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("TF_BUILD", "True")
	_, out, _ = runCLI(t, "", "check", "--base", "base")
	if !strings.Contains(out, "##vso[task.logissue type=error]") {
		t.Fatalf("out %q", out)
	}
	code, _, errb := runCLI(t, "", "check")
	if code != 2 || !strings.Contains(errb, "--base") {
		t.Fatalf("no base must exit 2 with a hint: %d %q", code, errb)
	}
	code, _, errb = runCLI(t, "", "check", "--base", "nope")
	if code != 2 || !strings.Contains(errb, "fetch-depth") {
		t.Fatalf("missing ref must hint fetch-depth: %d %q", code, errb)
	}
}

func TestCheckJSON(t *testing.T) {
	cliRepo(t)
	_, out, _ := runCLI(t, "scripts/a.sh\n", "--json", "check", "--stdin")
	var got struct {
		OK    bool  `json:"ok"`
		Fired []Hit `json:"fired"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if got.OK || len(got.Fired) != 1 || got.Fired[0].Name != "docs" {
		t.Fatalf("%+v", got)
	}
}

func TestWhy(t *testing.T) {
	cliRepo(t)
	code, out, _ := runCLI(t, "", "why", "scripts/x.sh", "auth/y.go")
	if code != 0 || !strings.Contains(out, "docs") || !strings.Contains(out, "SECURITY.md") {
		t.Fatalf("%d %q", code, out)
	}
	_, out, _ = runCLI(t, "", "why", "unrelated.txt")
	if !strings.Contains(out, "no rules") {
		t.Fatalf("%q", out)
	}
	_, out, _ = runCLI(t, "", "--json", "why", "scripts/x.sh")
	var rules []Rule
	if err := json.Unmarshal([]byte(out), &rules); err != nil || len(rules) != 1 || rules[0].Name != "docs" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestCoverageAndStaleCommands(t *testing.T) {
	r := cliRepo(t)
	code, out, _ := runCLI(t, "", "coverage")
	if code != 1 || !strings.Contains(out, "SECURITY.md") || !strings.Contains(out, "uncovered") {
		t.Fatalf("broken targets must exit 1: %d %q", code, out)
	}
	r.commit(t, 2000, map[string]string{"scripts/z.sh": "z"})
	code, out, _ = runCLI(t, "", "stale")
	if code != 1 || !strings.Contains(out, "docs") {
		t.Fatalf("scripts newer than README at same commit? %d %q", code, out)
	}
	r.commit(t, 3000, map[string]string{"README.md": "updated"})
	if code, out, _ = runCLI(t, "", "stale"); code != 0 {
		t.Fatalf("%d %q", code, out)
	}
}

func TestStaleAndCheatsheetRejectArgs(t *testing.T) {
	cliRepo(t)
	code, out, _ := runCLI(t, "", "stale", "--json")
	if code != 2 || out != "" {
		t.Fatalf("stale with trailing arg must exit 2 with no stdout: code %d out %q", code, out)
	}
	code, out, _ = runCLI(t, "", "cheatsheet", "extra")
	if code != 2 || out != "" {
		t.Fatalf("cheatsheet with trailing arg must exit 2 with no stdout: code %d out %q", code, out)
	}
}

func TestCheatsheetAndConfigFlag(t *testing.T) {
	r := cliRepo(t)
	alt := filepath.Join(r.dir, "alt.yml")
	os.WriteFile(alt, []byte("rules:\n  - {name: only, when: [a/], require: [b/]}\n"), 0o644)
	code, out, _ := runCLI(t, "", "--config", alt, "cheatsheet")
	if code != 0 || !strings.Contains(out, "| only |") {
		t.Fatalf("%d %q", code, out)
	}
	code, _, errb := runCLI(t, "", "--config", "missing.yml", "check", "--stdin")
	if code != 2 || errb == "" {
		t.Fatalf("missing config must exit 2: %d %q", code, errb)
	}
}

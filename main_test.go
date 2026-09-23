package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecisionCompileCommand(t *testing.T) {
	config := `rules:
  - name: choose
    when: [src/]
    require: [README.md]
    kernel:
      version: 1
      baseline: incumbent-v1
      utility: verified-progress-v1
      horizon_steps: 4
      features: [progress]
      actions: [read_context, run_tests]
      theta_q: [10000]
      weights_q: [[10000], [10000]]
      limits:
        candidates: 16
        features: 8
        selected: 1
        evaluator_calls: 0
        evaluator_parallel: 0
        evaluator_tokens: 0
        decision_timeout_ms: 0
        think_repeats: 0
`
	path := filepath.Join(t.TempDir(), "tennyn.yml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, "", "--config", path, "decision", "compile", "--rule", "choose")
	if code != 0 || !strings.Contains(out, `"digest":"`) || !strings.Contains(out, `"weights_q":[[10000],[10000]]`) {
		t.Fatalf("decision compile: code %d out %q err %q", code, out, errb)
	}
}

func TestKernelDoesNotChangeCheckResults(t *testing.T) {
	plain := `rules:
  - name: choose
    when: [src/]
    require: [README.md]
`
	withKernel := decisionFixture(t)
	plainPath := filepath.Join(t.TempDir(), "plain.yml")
	kernelPath := filepath.Join(t.TempDir(), "kernel.yml")
	if err := os.WriteFile(plainPath, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernelPath, []byte(withKernel), 0o644); err != nil {
		t.Fatal(err)
	}
	codePlain, outPlain, _ := runCLI(t, "src/a.go\n", "--config", plainPath, "check", "--stdin")
	codeKernel, outKernel, _ := runCLI(t, "src/a.go\n", "--config", kernelPath, "check", "--stdin")
	if codePlain != codeKernel || outPlain != outKernel {
		t.Fatalf("kernel changed check: plain %d %q, kernel %d %q", codePlain, outPlain, codeKernel, outKernel)
	}
}

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
		"tennyn.yml":   gateYAML,
		"README.md":    "hello",
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
	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "")
	t.Setenv("CI_MERGE_REQUEST_LABELS", "")
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

func TestWhyBase(t *testing.T) {
	r := cliRepo(t)
	r.git("branch", "base")
	r.commit(t, 2000, map[string]string{"scripts/b.sh": "y"})
	code, out, _ := runCLI(t, "", "why", "--base", "base")
	if code != 0 || !strings.Contains(out, `rule "docs"`) {
		t.Fatalf("why --base must report the fired rule: code %d out %q", code, out)
	}

	code, _, errb := runCLI(t, "", "why", "--base", "base", "--stdin")
	if code != 2 || errb == "" {
		t.Fatalf("--base with --stdin must exit 2: code %d errb %q", code, errb)
	}
	code, _, errb = runCLI(t, "", "why", "--base", "base", "extra/path")
	if code != 2 || errb == "" {
		t.Fatalf("--base with positional paths must exit 2: code %d errb %q", code, errb)
	}
}

func TestCoverageAndStaleCommands(t *testing.T) {
	r := cliRepo(t)
	code, out, _ := runCLI(t, "", "coverage")
	if code != 1 || !strings.Contains(out, "SECURITY.md") || !strings.Contains(out, "uncovered") {
		t.Fatalf("broken targets must exit 1: %d %q", code, out)
	}
	r.commit(t, 1000+90000, map[string]string{"scripts/z.sh": "z"})
	code, out, _ = runCLI(t, "", "stale")
	if code != 1 || !strings.Contains(out, "docs") {
		t.Fatalf("scripts a day+ newer than README? %d %q", code, out)
	}
	r.commit(t, 1000+91000, map[string]string{"README.md": "updated"})
	if code, out, _ = runCLI(t, "", "stale"); code != 0 {
		t.Fatalf("%d %q", code, out)
	}
}

func TestIgnoreCoverageOnly(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 1000, map[string]string{
		"tennyn.yml":   gateYAML + "ignore: [scripts/]\n",
		"README.md":    "hello",
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

	// check must still fire on an ignored path: ignore is coverage-only.
	code, out, _ := runCLI(t, "scripts/a.sh\n", "check", "--stdin")
	if code != 1 || !strings.Contains(out, `rule "docs"`) {
		t.Fatalf("ignore must not affect check: code %d out %q", code, out)
	}

	code, out, _ = runCLI(t, "", "coverage")
	if code != 1 {
		t.Fatalf("broken targets still exit 1: %d", code)
	}
	if !strings.Contains(out, "ignored") {
		t.Fatalf("text output must mention ignored count: %q", out)
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

func TestCheatsheetCheck(t *testing.T) {
	r := cliRepo(t)
	table := Cheatsheet(mustCfg(t, gateYAML))
	upToDate := filepath.Join(r.dir, "UPTODATE.md")
	os.WriteFile(upToDate, []byte("# Title\n\n"+table+"\nmore text\n"), 0o644)
	code, out, _ := runCLI(t, "", "cheatsheet", "--check", upToDate)
	if code != 0 || out != "" {
		t.Fatalf("up-to-date file must pass silently: code %d out %q", code, out)
	}

	// CRLF and trailing whitespace must be normalized away.
	crlf := strings.ReplaceAll(table, "\n", "\r\n")
	crlfFile := filepath.Join(r.dir, "CRLF.md")
	os.WriteFile(crlfFile, []byte("intro\r\n"+crlf+"trailer\r\n"), 0o644)
	code, _, _ = runCLI(t, "", "cheatsheet", "--check", crlfFile)
	if code != 0 {
		t.Fatalf("CRLF file must still be considered up to date: code %d", code)
	}

	stale := filepath.Join(r.dir, "STALE.md")
	staleTable := strings.Replace(table, "docs-unaffected", "something-else", 1)
	os.WriteFile(stale, []byte(staleTable), 0o644)
	code, out, _ = runCLI(t, "", "cheatsheet", "--check", stale)
	if code != 1 || !strings.Contains(out, "cheat sheet in "+stale+" is out of date") || !strings.Contains(out, "tennyn cheatsheet") {
		t.Fatalf("stale table must fail with a hint: code %d out %q", code, out)
	}

	missing := filepath.Join(r.dir, "MISSING.md")
	code, _, errb := runCLI(t, "", "cheatsheet", "--check", missing)
	if code != 2 || errb == "" {
		t.Fatalf("missing file must exit 2 with a message: code %d errb %q", code, errb)
	}

	code, out, _ = runCLI(t, "", "--json", "cheatsheet", "--check", upToDate)
	var got struct {
		OK   bool   `json:"ok"`
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || !got.OK || got.File != upToDate {
		t.Fatalf("json: %v %q", err, out)
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

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newTestRepo creates an empty git repo with a fixed identity in a temp dir.
func newTestRepo(t *testing.T) repo {
	t.Helper()
	dir := t.TempDir()
	r := repo{dir: dir}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "core.autocrlf", "false"},
	} {
		if _, err := r.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// commit writes files and commits them with the given unix timestamp.
func (r repo) commit(t *testing.T, ts int64, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(r.dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.git("add", "-A"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", r.dir, "commit", "-q", "-m", fmt.Sprintf("ts %d", ts))
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GIT_AUTHOR_DATE=@%d +0000", ts),
		fmt.Sprintf("GIT_COMMITTER_DATE=@%d +0000", ts))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
}

func TestChangedAndTracked(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 1000, map[string]string{"README.md": "a", "scripts/a.sh": "x"})
	if _, err := r.git("branch", "base"); err != nil {
		t.Fatal(err)
	}
	r.commit(t, 2000, map[string]string{"scripts/b.sh": "y", "docs/g.md": "z"})
	got, err := r.changed("base")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"docs/g.md", "scripts/b.sh"}) {
		t.Fatalf("changed = %v", got)
	}
	tracked, err := r.tracked()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 4 || tracked[0] != "README.md" {
		t.Fatalf("tracked = %v", tracked)
	}
	root, err := r.root()
	if err != nil || filepath.Clean(root) != filepath.Clean(r.dir) {
		t.Fatalf("root = %q err %v", root, err)
	}
}

func TestDetectBase(t *testing.T) {
	t.Setenv("GITHUB_BASE_REF", "")
	t.Setenv("SYSTEM_PULLREQUEST_TARGETBRANCH", "")
	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "")
	if b := detectBase(); b != "" {
		t.Fatalf("want empty, got %q", b)
	}
	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "main")
	if b := detectBase(); b != "origin/main" {
		t.Fatalf("gitlab: got %q", b)
	}
	t.Setenv("SYSTEM_PULLREQUEST_TARGETBRANCH", "refs/heads/main")
	if b := detectBase(); b != "origin/main" {
		t.Fatalf("ado wins over gitlab: got %q", b)
	}
	t.Setenv("GITHUB_BASE_REF", "develop")
	if b := detectBase(); b != "origin/develop" {
		t.Fatalf("github wins: got %q", b)
	}
}

func TestLabelsFromEnv(t *testing.T) {
	t.Setenv("TENNYN_LABELS", "a, b")
	t.Setenv("SYSTEM_PULLREQUEST_PULLREQUESTID", "")
	if got := labels(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
	t.Setenv("TENNYN_LABELS", "")
	if got := labels(); len(got) != 0 {
		t.Fatalf("empty env must mean no labels, got %v", got)
	}
}

func TestGitLabLabelsDoNotShadowExplicitEmptyTennynLabels(t *testing.T) {
	t.Setenv("TENNYN_LABELS", "")
	t.Setenv("CI_MERGE_REQUEST_LABELS", "a,b")
	if got := labels(); len(got) != 0 {
		t.Fatalf("explicit empty TENNYN_LABELS must win over GitLab labels, got %v", got)
	}
}

func TestLabelsFromGitLab(t *testing.T) {
	os.Unsetenv("TENNYN_LABELS")
	t.Setenv("SYSTEM_PULLREQUEST_PULLREQUESTID", "")
	t.Setenv("CI_MERGE_REQUEST_LABELS", "a, b")
	if got := labels(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
}

func TestStale(t *testing.T) {
	r := newTestRepo(t)
	cfg := mustCfg(t, "rules:\n  - name: docs\n    when: [scripts/]\n    require: [docs/]\n  - name: never\n    when: [nope/]\n    require: [docs/]\n")
	r.commit(t, 1_000_000, map[string]string{"scripts/a.sh": "1"})
	r.commit(t, 2_000_000, map[string]string{"docs/g.md": "<!-- verified: 1970-01-01 verifies-against: abc1234 -->\n"})
	s, err := Stale(cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	if s[0].Stale || s[0].WhenTS != 1_000_000 || s[0].RequireTS != 2_000_000 {
		t.Fatalf("fresh case wrong: %+v", s[0])
	}
	if s[0].FreshBy != "docs/" {
		t.Fatalf("fresh case must name the winning require pattern: %+v", s[0])
	}
	if s[1].WhenTS != 0 || s[1].Stale {
		t.Fatalf("never-touched when must not be stale: %+v", s[1])
	}
	// A same-day lag (code lands an hour after docs) is fresh, not stale.
	r.commit(t, 2_003_600, map[string]string{"scripts/a.sh": "2"})
	s, _ = Stale(cfg, r)
	if s[0].Stale || s[0].LagDays != 0 {
		t.Fatalf("sub-day lag must not be stale: %+v", s[0])
	}
	r.commit(t, 3_000_000, map[string]string{"scripts/a.sh": "3"})
	s, _ = Stale(cfg, r)
	if !s[0].Stale || s[0].LagDays != 11 {
		t.Fatalf("stale case wrong: %+v", s[0])
	}
	if s[0].FreshBy != "" {
		t.Fatalf("stale rule must not report fresh_by: %+v", s[0])
	}
	// A verified: header newer than the commit rescues the rule.
	r.commit(t, 3_500_000, map[string]string{"docs/g.md": "<!-- verified: 2030-01-01 verifies-against: abc1234 -->\n"})
	r.commit(t, 4_000_000, map[string]string{"scripts/a.sh": "4"})
	s, _ = Stale(cfg, r)
	if s[0].Stale {
		t.Fatalf("verified header must win: %+v", s[0])
	}
	if s[0].FreshBy != "verified: docs/g.md" {
		t.Fatalf("verified case must name the file: %+v", s[0])
	}
}

// TestStaleFreshByPicksNewestRequirePattern uses two require patterns so
// FreshBy must name whichever one's newest commit actually is the newest,
// not just the first pattern listed.
func TestStaleFreshByPicksNewestRequirePattern(t *testing.T) {
	r := newTestRepo(t)
	cfg := mustCfg(t, "rules:\n  - name: docs\n    when: [scripts/]\n    require: [README.md, docs/]\n")
	r.commit(t, 1_000_000, map[string]string{"scripts/a.sh": "1", "README.md": "1"})
	r.commit(t, 2_000_000, map[string]string{"docs/g.md": "1"})
	s, err := Stale(cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	if s[0].FreshBy != "docs/" || s[0].RequireTS != 2_000_000 {
		t.Fatalf("must pick the pattern with the newest commit: %+v", s[0])
	}
}

func TestVerifiedDateBareLine(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 1_000_000, map[string]string{"docs/bare.md": "verified: 2030-01-01\nsome other content\n"})
	got := r.verifiedDate([]string{"docs/bare.md"})
	want, _ := time.Parse("2006-01-02", "2030-01-01")
	if got != want.Unix() {
		t.Fatalf("bare verified line: got %d want %d", got, want.Unix())
	}
}

func TestNewestResolvesPerGroup(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 10, map[string]string{"a": "1"})
	r.commit(t, 20, map[string]string{"b": "1"})
	pa, _ := compilePattern("a")
	pb, _ := compilePattern("b")
	got, err := r.newest([][]Pattern{{pa}, {pb}, {mustPat("zzz")}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int64{10, 20, 0}) {
		t.Fatalf("got %v", got)
	}
}

// TestNewestEarlyExitCorrectness forces newest to scan almost the entire
// history: the only commit touching "a" is the very first (oldest) one, and
// only "a" is asked for, so pending stays 1 until the last line is read.
func TestNewestEarlyExitCorrectness(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 1, map[string]string{"a": "1"})
	for i := 2; i <= 30; i++ {
		r.commit(t, int64(i), map[string]string{"other": fmt.Sprintf("%d", i)})
	}
	pa, _ := compilePattern("a")
	got, err := r.newest([][]Pattern{{pa}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("got %v want [1]", got)
	}
}

func TestNewestAtSignPath(t *testing.T) {
	r := newTestRepo(t)
	r.commit(t, 10, map[string]string{"@scope/a.ts": "1"})
	r.commit(t, 20, map[string]string{"b": "1"})
	got, err := r.newest([][]Pattern{{mustPat("@scope/")}, {mustPat("b")}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int64{10, 20}) {
		t.Fatalf("got %v", got)
	}
}

func mustPat(s string) Pattern {
	p, err := compilePattern(s)
	if err != nil {
		panic(err)
	}
	return p
}

func TestADOLabels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotPath = req.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"name": "docs-unaffected"}, {"name": "x"}}})
	}))
	defer srv.Close()
	os.Unsetenv("TENNYN_LABELS")
	t.Setenv("CI_MERGE_REQUEST_LABELS", "")
	t.Setenv("SYSTEM_COLLECTIONURI", srv.URL+"/")
	t.Setenv("SYSTEM_TEAMPROJECT", "My Project")
	t.Setenv("BUILD_REPOSITORY_ID", "repo-guid")
	t.Setenv("SYSTEM_PULLREQUEST_PULLREQUESTID", "42")
	t.Setenv("SYSTEM_ACCESSTOKEN", "tok")
	got := labels()
	if !reflect.DeepEqual(got, []string{"docs-unaffected", "x"}) {
		t.Fatalf("got %v", got)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth %q", gotAuth)
	}
	want := "/My%20Project/_apis/git/repositories/repo-guid/pullRequests/42/labels?api-version=7.1"
	if !strings.HasSuffix(gotPath, want) {
		t.Fatalf("path %q want suffix %q", gotPath, want)
	}
}

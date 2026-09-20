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
	if b := detectBase(); b != "" {
		t.Fatalf("want empty, got %q", b)
	}
	t.Setenv("SYSTEM_PULLREQUEST_TARGETBRANCH", "refs/heads/main")
	if b := detectBase(); b != "origin/main" {
		t.Fatalf("ado: got %q", b)
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

func TestADOLabels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotPath = req.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"name": "docs-unaffected"}, {"name": "x"}}})
	}))
	defer srv.Close()
	os.Unsetenv("TENNYN_LABELS")
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

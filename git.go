package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type repo struct{ dir string }

func (r repo) git(args ...string) (string, error) {
	full := append([]string{"-c", "core.quotePath=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r repo) root() (string, error) { return r.git("rev-parse", "--show-toplevel") }

// changed lists paths touched between the merge base of base and HEAD, and HEAD.
func (r repo) changed(base string) ([]string, error) {
	out, err := r.git("diff", "--name-only", base+"...HEAD")
	if err != nil {
		return nil, fmt.Errorf("%w (is %q fetched? CI checkouts need fetch-depth: 0)", err, base)
	}
	return splitLines(out), nil
}

func (r repo) tracked() ([]string, error) {
	out, err := r.git("ls-files")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// detectBase reads the PR target branch from GitHub/Forgejo or Azure DevOps.
func detectBase() string {
	if v := os.Getenv("GITHUB_BASE_REF"); v != "" {
		return "origin/" + v
	}
	if v := os.Getenv("SYSTEM_PULLREQUEST_TARGETBRANCH"); v != "" {
		return "origin/" + strings.TrimPrefix(v, "refs/heads/")
	}
	return ""
}

// labels returns PR labels: TENNYN_LABELS if set (even empty), else fetched
// from Azure DevOps when its PR variables are present, else none.
func labels() []string {
	if v, ok := os.LookupEnv("TENNYN_LABELS"); ok {
		return splitLabels(v)
	}
	if os.Getenv("SYSTEM_PULLREQUEST_PULLREQUESTID") != "" && os.Getenv("SYSTEM_ACCESSTOKEN") != "" {
		ls, err := adoLabels()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tennyn: warning: could not fetch Azure DevOps labels, treating as none: %v\n", err)
			return nil
		}
		return ls
	}
	return nil
}

// adoLabels calls the Azure DevOps PR labels API with the build's own token.
func adoLabels() ([]string, error) {
	base := os.Getenv("SYSTEM_COLLECTIONURI")
	if base == "" {
		return nil, errors.New("SYSTEM_COLLECTIONURI unset")
	}
	u := strings.TrimRight(base, "/") + "/" + url.PathEscape(os.Getenv("SYSTEM_TEAMPROJECT")) +
		"/_apis/git/repositories/" + url.PathEscape(os.Getenv("BUILD_REPOSITORY_ID")) +
		"/pullRequests/" + url.PathEscape(os.Getenv("SYSTEM_PULLREQUEST_PULLREQUESTID")) +
		"/labels?api-version=7.1"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("SYSTEM_ACCESSTOKEN"))
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Value))
	for _, v := range body.Value {
		out = append(out, v.Name)
	}
	return out, nil
}

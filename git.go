package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
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

// newest streams `git log --name-only` newest-first and returns, per group,
// the committer timestamp of the first commit touching any path matching any
// pattern in the group (0 if none). It stops reading as soon as every group
// is resolved, so on most repos it reads a few hundred commits, not the history.
// The header line is prefixed with a NUL byte (a byte no tracked path can
// contain) rather than '@', so paths like "@scope/pkg" are never mistaken
// for a timestamp header.
func (r repo) newest(groups [][]Pattern) ([]int64, error) {
	out := make([]int64, len(groups))
	pending := len(groups)
	cmd := exec.Command("git", "-c", "core.quotePath=false", "log", "--format=%x00%ct", "--name-only")
	cmd.Dir = r.dir
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var ts int64
	for sc.Scan() && pending > 0 {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
		case line[0] == 0:
			ts, _ = strconv.ParseInt(line[1:], 10, 64)
		default:
			for i, g := range groups {
				if out[i] == 0 && matchAny(g, line) {
					out[i] = ts
					pending--
				}
			}
		}
	}
	scErr := sc.Err()
	_ = cmd.Process.Kill() // early exit is the normal path; git's exit status is irrelevant here
	_ = cmd.Wait()
	if scErr != nil {
		return nil, scErr
	}
	return out, nil
}

var verifiedRe = regexp.MustCompile(`(?m)^[^\w\n]{0,8}verified:\s*(\d{4}-\d{2}-\d{2})`)

// verifiedDate returns the newest `verified: YYYY-MM-DD` header found in the
// first 2 KiB of the given tracked files, as a unix timestamp (0 if none).
func (r repo) verifiedDate(files []string) int64 {
	var best int64
	buf := make([]byte, 2048)
	for _, f := range files {
		fh, err := os.Open(filepath.Join(r.dir, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		n, _ := io.ReadFull(fh, buf)
		fh.Close()
		for _, m := range verifiedRe.FindAllSubmatch(buf[:n], -1) {
			if t, err := time.Parse("2006-01-02", string(m[1])); err == nil && t.Unix() > best {
				best = t.Unix()
			}
		}
	}
	return best
}

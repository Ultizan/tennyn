# Agent guidance

This repo is a Go CLI (`package main`, four source files). Run `go vet ./... && go test ./...` before every commit; the tests spin up throwaway git repos and need `git` on PATH.

## Using tennyn from an agent (in any repo that has a `tennyn.yml`)

- Before editing: `tennyn --json why <paths you intend to touch>` tells you which docs or contract files must change in the same PR, and who owns them. Do that work in the same change; do not plan to apply the waiver label unless the operator says so.
- When asked about doc or contract health: `tennyn --json stale` (what has drifted) and `tennyn --json coverage` (what nothing watches, and rules pointing at files that no longer exist).
- Before opening a PR: `git diff --name-only origin/main...HEAD | tennyn check --stdin` reproduces the CI gate locally.

## Changing tennyn itself

- Behaviour or wrapper changes require a README or AGENTS update in the same PR (this repo's own `tennyn.yml` enforces it).
- Keep the two dependencies. Prefer deleting over adding.
- Releases: tag `vX.Y.Z` on main; CI publishes binaries to Forgejo and GitHub and moves the floating `vX` tag.

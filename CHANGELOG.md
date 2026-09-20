# Changelog

## v1.0.2 — tennyn 1.0: the docs tripwire, graduated (2026-09-20)

**tennyn** (Welsh: *tether*) is a co-change gate for CI: **when these paths change, those paths must change too**, or the pull request carries a waiver label. Docs must move with the code, the threat model with auth, the OpenAPI spec with the handlers. One static binary, one YAML file, GitHub, Forgejo and Azure DevOps.

This is the 1.0 of a gate that has been running in production since July 2026 across the Bonsai Collective's repos (currently private) as eight hand-copied shell scripts, where it has caught doc drift on every large clean-up since. tennyn is that contract with the duplication removed, the waiver path made auditable, and analysis added on top. Like [check-for-changed-files](https://github.com/brettcannon/check-for-changed-files), plus Azure DevOps and Forgejo, plus coverage, staleness and a cheat sheet from one config.

### Use it

```yaml
# .github/workflows/tennyn.yml (identical on Forgejo)
on: { pull_request: { types: [opened, synchronize, reopened, labeled, unlabeled] } }
jobs:
  tennyn:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: Ultizan/tennyn@v1
```

```yaml
# tennyn.yml at the repo root
rules:
  - name: docs
    when: [src/, Makefile]
    require: [docs/, README.md]
    why: user-facing behaviour lives in docs
    bypass: docs-unaffected      # optional PR label that waives this rule; always printed as an audit line
```

Azure DevOps consumers use the step template in `azure-pipelines/`. Locally or from an agent: `curl -fsSL https://github.com/Ultizan/tennyn/releases/latest/download/install.sh | sh`.

### What you get

- `tennyn check` — the gate. Auto-detects the PR base on GitHub, Forgejo and Azure DevOps; per-rule waiver labels; CI error annotations; exit 0 / 1 / 2.
- `tennyn why <paths>` — before you edit: which rules fire and what you must also update, with owners.
- `tennyn coverage` — which files no rule watches, rules that watch nothing, `require` targets that no longer exist.
- `tennyn stale` — rules whose watched paths moved at least a day after their required paths; honours a `verified: YYYY-MM-DD` freshness header, including inside an HTML comment.
- `tennyn cheatsheet` — the rules as a markdown table for your CONTRIBUTING or AGENTS file.
- `--json` on everything, for agents and scripts.

### What 1.x promises

The `tennyn.yml` schema, the exit codes (0 pass, 1 violation, 2 config or git error) and the `--json` shapes change only with a major. `@v1` always installs the newest 1.x binary; the root `VERSION` file pins it, so the floating tag can never jump a major.

### Assets

Static binaries for linux/darwin (amd64, arm64) and windows (amd64), `SHA256SUMS`, and `install.sh`. Built by CI with `-trimpath`; hashes are reproducible.

### Changes since v1.0.1

- `stale`: a lag under one day between watched and required paths now reads as fresh (was reported as "STALE by 0 days").
- The `uses: ./` self-test in CI now pins v1.0.1.

MIT licensed. Issues and PRs welcome.

## v1.0.1 (2026-09-20)

- `verified:` freshness headers are recognised inside HTML comments (`<!-- verified: YYYY-MM-DD ... -->`), the form the Bonsai Collective repos use.
- `VERSION` file pins the binary that `@v1` installs; CI refuses a tag whose VERSION differs; the release job is gated on a dotted tag.
- `stale` and `cheatsheet` reject misplaced flags instead of ignoring them.

## v1.0.0 (2026-09-20)

- First release: `check`, `why`, `coverage`, `stale`, `cheatsheet`; GitHub/Forgejo composite action; Azure DevOps step template; `install.sh`.

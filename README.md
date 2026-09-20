# tennyn

*Welsh: tether.* A co-change gate for CI: **when these paths change, those must change too**, or the PR carries a waiver label. One static binary, one YAML file, runs on GitHub, Forgejo and Azure DevOps. Like [check-for-changed-files](https://github.com/brettcannon/check-for-changed-files), plus Azure DevOps and Forgejo, plus coverage, staleness and a cheat sheet from one config.

Typical rules: infra changed → runbook updated; auth code changed → threat model touched; handler changed → OpenAPI spec changed; schema changed → migration added.

## Quick start

1. Add `tennyn.yml` at the repo root:

   ```yaml
   rules:
     - name: docs
       when: [src/, Makefile]
       require: [docs/, README.md]
       why: user-facing behaviour lives in docs
       owner: maintainers
       bypass: docs-unaffected      # optional PR label that waives this rule (audited, never silent)
   ```

2. Wire the gate.

   **GitHub / Forgejo** (`.github/workflows/tennyn.yml` or `.forgejo/workflows/tennyn.yml`):
   ```yaml
   on: { pull_request: { types: [opened, synchronize, reopened, labeled, unlabeled] } }
   jobs:
     tennyn:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v7
           with: { fetch-depth: 0 }
         - uses: Ultizan/tennyn@v1
   ```
   On Forgejo, reference the action by full URL if your runner resolves `uses:` elsewhere: `uses: https://github.com/Ultizan/tennyn@v1`. Air-gapped? Set `download-url` to your Forgejo's `/releases` and pin `version`. That Forgejo instance must allow anonymous release downloads — a lab instance that redirects `/releases` to a sign-in page will not work.

   **Azure DevOps**:
   ```yaml
   resources:
     repositories:
       - repository: tennyn
         type: github
         name: Ultizan/tennyn
         ref: refs/tags/v1.0.0
         endpoint: github
   steps:
     - template: azure-pipelines/tennyn.yml@tennyn
   ```
   Labels are read through the build's own `System.AccessToken`; grant the build service *Read* on the repo (it usually already has it).

3. Locally: `curl -fsSL https://github.com/Ultizan/tennyn/releases/latest/download/install.sh | sh` or `go install github.com/Ultizan/tennyn@latest` (this reports `tennyn dev`; `version` is only stamped into release binaries).

## Commands

| Command | What it answers | Exit 1 when |
|---|---|---|
| `tennyn check [--base REF] [--stdin]` | Does this change satisfy every rule it fires? Base is auto-detected on GitHub, Forgejo and ADO PR builds. | a fired rule is neither satisfied nor waived |
| `tennyn why PATH...` | If I touch these, what must I also update, and who owns it? | never |
| `tennyn coverage [--all]` | Which files does no rule watch? Which rules watch nothing? Which `require` targets no longer exist? | a require target matches no tracked file |
| `tennyn stale` | Which rules' watched paths last moved at least a day after their required paths did? Honors a `verified: YYYY-MM-DD` header, optionally inside an HTML comment, within the first 2 KiB of a required file. | any rule is stale |
| `tennyn cheatsheet` | The rules as a markdown table for your CONTRIBUTING or AGENTS file. | never |

Every command takes `--json` (before the command) for machine use and `--config PATH` to point at a different rules file. Exit 2 means a config or git problem and the message says which.

## Config reference

| Key | Required | Meaning |
|---|---|---|
| `rules[].name` | yes | unique id; shown in output |
| `rules[].when` | yes | patterns that fire the rule |
| `rules[].require` | yes | patterns, any of which satisfies the rule when also changed |
| `rules[].why` | no | one line shown on failure and in the cheat sheet |
| `rules[].owner` | no | shown in `why` and the cheat sheet |
| `rules[].bypass` | no | PR label that waives this rule; absent means no waiver |
| `anchors` | no | scratch space for YAML anchors; ignored |

Patterns are anchored at the repo root. `dir/` means everything under `dir`; a plain path is exact; globs use `*`, `?`, `[..]`, `{a,b}` and `**`. Lists may nest and are flattened, so `require: [RUNBOOK-*.md, *shared-docs]` works with a YAML anchor.

## This repo's own rules

| Rule | If you touch | Then update (any of) | Why | Owner | Waiver label |
|---|---|---|---|---|---|
| docs | `*.go`, `action.yml`, `azure-pipelines/`, `install.sh` | `README.md`, `AGENTS.md` | behaviour or wrapper changes must be reflected in the docs | maintainer | `docs-unaffected` |

## Design notes

- Changed files come from `git diff --name-only <base>...HEAD`; CI checkouts need `fetch-depth: 0`.
- Waivers are per rule: a docs waiver never waives a security rule.
- `stale` streams `git log --name-only` newest-first and stops as soon as every rule is resolved, so it is fast on large histories. A rule is stale only once its watched paths last moved at least a day (86400s) after its required paths did; a same-day lag is fresh.
- The floating `vN` tag pins the action code; the root `VERSION` file pins the binary tag the action installs when `inputs.version` is unset. CI refuses to release a tag whose `VERSION` differs, so `@v1` never silently jumps a major.
- `scripts/release.sh` needs `jq` on the runner.
- Not in scope: changelog conventions, history mining, language-aware rules. Other tools do those well.

MIT licensed.

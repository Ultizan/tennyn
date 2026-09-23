# tennyn

*Welsh: tether.* A co-change gate for CI: **when these paths change, those must change too**, or the PR carries a waiver label. One static binary, one YAML file, runs on GitHub, Forgejo and Azure DevOps. Like [check-for-changed-files](https://github.com/brettcannon/check-for-changed-files), plus Azure DevOps and Forgejo, plus coverage, staleness and a cheat sheet from one config.

Typical rules: infra changed → runbook updated; auth code changed → threat model touched; handler changed → OpenAPI spec changed; schema changed → migration added.

tennyn is a Bonsai Collective tool. It grew out of the docs tripwire that guarded the collective's repos (Nemeton, Small Works, Teire, Sylfaen and the umbrella, currently private) as eight hand-copied shell scripts, and now gates all of them from one binary. Available on the GitHub Marketplace as "tennyn co-change gate" (`uses: Ultizan/tennyn@v1`).

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
         - uses: actions/checkout@v4
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
         ref: refs/tags/v1.0.2
         endpoint: github
   steps:
     - template: azure-pipelines/tennyn.yml@tennyn
   ```
   Labels are read through the build's own `System.AccessToken`; grant the build service *Read* on the repo (it usually already has it).

   **GitLab CI** (`.gitlab-ci.yml`):
   ```yaml
   tennyn:
     rules: [{ if: '$CI_PIPELINE_SOURCE == "merge_request_event"' }]
     script: [ "curl -fsSL https://github.com/Ultizan/tennyn/releases/latest/download/install.sh | sh", "tennyn check" ]
   ```
   Base and labels are read from `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` and `CI_MERGE_REQUEST_LABELS`, which GitLab sets on merge-request pipelines.

3. Locally: `curl -fsSL https://github.com/Ultizan/tennyn/releases/latest/download/install.sh | sh` or `go install github.com/Ultizan/tennyn@latest` (this reports `tennyn dev`; `version` is only stamped into release binaries).

## Commands

| Command | What it answers | Exit 1 when |
|---|---|---|
| `tennyn check [--base REF] [--stdin]` | Does this change satisfy every rule it fires? Base is auto-detected on GitHub, Forgejo, ADO and GitLab PR/MR builds. | a fired rule is neither satisfied nor waived |
| `tennyn why [--base REF \| --stdin] PATH...` | If I touch these (or everything a REF diff touches), what must I also update, and who owns it? `--base` is mutually exclusive with `--stdin` and PATH... | never |
| `tennyn coverage [--all]` | Which files does no rule watch? Which rules watch nothing? Which `require` targets no longer exist? Files matching the config's `ignore:` list are excluded and reported as `ignored`. | a require target matches no tracked file |
| `tennyn stale` | Which rules' watched paths last moved at least a day after their required paths did? Honors a `verified: YYYY-MM-DD` header, optionally inside an HTML comment, within the first 2 KiB of a required file. A fresh, ever-touched rule also reports `fresh_by`: the `require` pattern (or `verified: <file>`) that kept it fresh. | any rule is stale |
| `tennyn cheatsheet [--check FILE]` | The rules as a markdown table for your CONTRIBUTING or AGENTS file, or (`--check`) whether FILE already contains that exact table. | never for the plain table; with `--check`, FILE does not contain the table (exit 2 if FILE is unreadable) |
| `tennyn decision compile --rule NAME` | Compile that rule's optional version-1 decision kernel to canonical JSON with a SHA-256 digest. | the config is invalid or the rule is missing, ambiguous or has no kernel (exit 2) |

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
| `rules[].kernel` | no | strict version-1 decision contract used only by `decision compile`; co-change commands ignore scores |
| `ignore` | no | patterns excluded from `coverage`'s totals and `uncovered`/`uncovered_files` (reported as `ignored`); does not affect `check`, `why`, `stale`, dead rules or broken targets |
| `anchors` | no | scratch space for YAML anchors; ignored |

A rule may carry an optional decision contract. Compile it explicitly with `tennyn --config tennyn.yml decision compile --rule choose`; this emits compact JSON in fixed field order. The SHA-256 digest covers that canonical JSON without the `digest` field. Version 1 requires unique nonempty action and feature IDs (1–16 actions, 1–8 features), `limits.features` as a capacity at least as large as the feature list (up to 8), `selected: 1`, positive `horizon_steps`, theta values in [-10000, 10000], weights in [0, 10000], and nonnegative signed-64-bit limits. Unknown or duplicate kernel keys, YAML aliases inside `kernel`, extra YAML documents, fractional numbers, and mismatched matrix axes are rejected. A kernel does not alter `check`, `why`, or other co-change results.

For example, `weights_q` rows follow the `actions` order and each row follows `features` order:

```yaml
rules:
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
        evaluator_calls: 3
        evaluator_parallel: 2
        evaluator_tokens: 768
        decision_timeout_ms: 5000
        think_repeats: 1
```

Patterns are anchored at the repo root. `dir/` means everything under `dir`; a plain path is exact; globs use `*`, `?`, `[..]`, `{a,b}` and `**`. Lists may nest and are flattened, so `require: [RUNBOOK-*.md, *shared-docs]` works with a YAML anchor.

## This repo's own rules

| Rule | If you touch | Then update (any of) | Why | Owner | Waiver label |
|---|---|---|---|---|---|
| docs | `*.go`, `action.yml`, `azure-pipelines/`, `install.sh`, `tennyn.yml` | `README.md`, `AGENTS.md` | behaviour, wrapper or rule changes must be reflected in the docs (rule changes refresh the cheat sheet) | maintainer | `docs-unaffected` |
| changelog | `VERSION` | `CHANGELOG.md` | every release gets a changelog entry | maintainer |  |
| readme-pins | `VERSION` | `README.md` | the README pins the release tag in examples; re-check them on every release | maintainer |  |

## Privacy

tennyn collects no data and sends nothing anywhere. The action downloads a release binary from GitHub's release CDN (or the `download-url` you set), then everything runs inside your own runner against your own checkout. On Azure DevOps it reads pull-request labels through your own project's API with the build's own token. There is no telemetry, no crash reporting, and no network call the binary makes on its own.

## Design notes

- Changed files come from `git diff --name-only <base>...HEAD`; CI checkouts need `fetch-depth: 0`.
- Waivers are per rule: a docs waiver never waives a security rule.
- `stale` streams `git log --name-only` newest-first and stops as soon as every rule is resolved, so it is fast on large histories. A rule is stale only once its watched paths last moved at least a day (86400s) after its required paths did; a same-day lag is fresh.
- The floating `vN` tag pins the action code; the root `VERSION` file pins the binary tag the action installs when `inputs.version` is unset. CI refuses to release a tag whose `VERSION` differs, so `@v1` never silently jumps a major.
- Versioning: 1.x is early for a tool this young, but the `@v1` pin needs a major, and what 1.x promises is the mechanical contract, not feature maturity: the `tennyn.yml` schema, the exit codes (0 pass, 1 violation, 2 config or git error) and the `--json` shapes. Those change only with a major; everything else is free to move in minors.
- `install.sh` verifies the download against `SHA256SUMS` in either sha256sum line form (text or binary mode) and refuses to install when neither `sha256sum` nor `shasum` is available.
- `scripts/release.sh` needs `jq` on the runner.
- Not in scope: changelog conventions, history mining, language-aware rules. Other tools do those well.

MIT licensed.

# GitHub Actions

## Pinning actions

Always pin actions to **full commit SHAs**, never tags — tags are a supply-chain risk. Include the reference as a trailing comment. When adding a new action, look up the latest stable release tag on GitHub and resolve it to its commit SHA (`git ls-remote`). For annotated tags, use the dereferenced `^{}` SHA.

```yaml
- uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
```

## Step naming

Always name every step. Use the convention `[TYPE] | Action`:

```yaml
- name: "[Setup] | Checkout"
  uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3

- name: "[Setup] | Mise"
  uses: jdx/mise-action@dba19683ed58901619b14f395a24841710cb4925 # v4.1.0

- name: "[Build] | Compile binary"
  shell: bash -euo pipefail {0}
  run: go build -o bin/htmlup ./cmd/htmlup
```

## Comments

Step names already say *what* a step does — don't restate it in a comment. Add a comment only for non-obvious *why*: a gotcha or constraint not visible from the step itself (e.g. why a tag is pushed with an App token rather than `GITHUB_TOKEN`). Keep it to one line. When in doubt, omit.

## Shell steps

Always set `shell` explicitly. Use `bash -euo pipefail {0}` to fail on errors, unset variables, and broken pipes:

```yaml
- name: "[Test] | Run checks"
  shell: bash -euo pipefail {0}
  run: mise run check
```

## Run names

Every workflow sets `run-name:` dynamically so a run is identifiable at a glance
from the Actions list — `<workflow> <version-or-ref>`, e.g. `release v1.2.3`.
Workflow `name:` and `run-name:` are **lowercase** by convention.

```yaml
name: release
run-name: release ${{ inputs.tag }}
```

Use the most specific identifier available: the release tag for tag/dispatch
flows, otherwise `github.ref_name`. Reusable workflows (`workflow_call`-only) run
nested under the caller, so their `run-name` is cosmetic — still set it for when
they are dispatched directly.

## Workflows

| Workflow | Trigger | What |
|---|---|---|
| `check.yaml` | push/PR to `master`, reusable | lint (fmt + vet + golangci-lint + htmlhint + shellcheck) and test in parallel |
| `release.yaml` | manual (`workflow_dispatch`, `version` without `v`) | validate version → `check.yaml` → create and push the `v<version>` tag, then **stops**. The tag is pushed with a GitHub App token (`RELEASE_APP_*`), not `GITHUB_TOKEN`, so the push triggers `publish.yaml` (`GITHUB_TOKEN` pushes do not start workflow runs). |
| `publish.yaml` | `push` of a `v*` tag | builds from the tag — GoReleaser builds, signs (cosign + GPG), publishes the release (archives + standalone binaries + signed `SHA256SUMS`, `mode: replace`), and pushes the Homebrew cask to `truehhart/homebrew-tap` (cross-repo push uses an App token from `RELEASE_APP_*`, scoped to `homebrew-tap`) |

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

## Shell steps

Always set `shell` explicitly. Use `bash -euo pipefail {0}` to fail on errors, unset variables, and broken pipes:

```yaml
- name: "[Test] | Run checks"
  shell: bash -euo pipefail {0}
  run: mise run check
```

## Run names

Every workflow sets `run-name:` dynamically so a run is identifiable at a glance
from the Actions list — `<Workflow> <version-or-ref>`, e.g. `Release v1.2.3`.

```yaml
name: Release
run-name: Release ${{ inputs.tag }}
```

Use the most specific identifier available: the release tag for tag/dispatch
flows, otherwise `github.ref_name`. Reusable workflows (`workflow_call`-only) run
nested under the caller, so their `run-name` is cosmetic — still set it for when
they are dispatched directly.

## Workflows

| Workflow | Trigger | What |
|---|---|---|
| `check.yaml` | push/PR to `master`, `v*` tags, reusable | lint (fmt + vet + golangci-lint + htmlhint + shellcheck) and test in parallel |
| `publish.yaml` | reusable (`workflow_call`, `version` input) | pushes the `v<version>` tag + builds from it; GoReleaser builds + signs (no publish); `softprops/action-gh-release` releases the tag, generates notes, uploads artifacts |
| `release.yaml` | manual (`workflow_dispatch`, `version` without `v`) | orchestrator — validate version → `check.yaml` → `publish.yaml` (`needs: check`) |

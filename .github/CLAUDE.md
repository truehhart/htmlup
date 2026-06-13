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

## Workflows

| Workflow | Trigger | What |
|---|---|---|
| `check.yaml` | push/PR to `master`, `v*` tags, reusable | lint (fmt + vet + golangci-lint + htmlhint) and test in parallel |
| `publish.yaml` | reusable (`workflow_call`) | GoReleaser builds + signs + creates the GitHub release |
| `release.yaml` | `v*` tag push | orchestrator — calls `check.yaml`, then `publish.yaml` (`needs: check`) |

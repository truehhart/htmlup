# CLAUDE.md — htmlup

`htmlup` is a small **Go CLI** that uploads HTML pages and makes them publicly available. Two backends ship in the MVP: **GitHub Pages** (push to a repo, served by Pages) and **S3** (upload objects, exposed via CloudFront or, post-MVP, a built-in HTTP server). The repo is also a **Claude Code plugin marketplace** publishing a skill on how to drive the CLI.

**`docs/ARCHITECTURE.md` is the reference** for the command surface, the provider interface, auth model, and the GitHub Pages lifecycle story. Read it before implementing anything.

## Hard rules

- **Let official SDKs own auth — never hand-roll credential handling.** AWS goes through `aws-sdk-go-v2`'s default credential chain; GitHub through `go-github` + `golang.org/x/oauth2` (token from `GITHUB_TOKEN`/`GH_TOKEN` or the `gh` CLI). No custom token storage, no bespoke signing.
- **The publish path does no lifecycle management of uploaded files.** It uploads and exits — it never deletes. GitHub Pages cleanup is delegated to an opt-in cron GitHub Actions workflow in the *target* repo; `htmlup github setup` installs that workflow once (it does the deleting, on a schedule, in the target repo — see ARCHITECTURE). S3 lifecycle is the bucket owner's problem.
- **Backends are pluggable.** Every target implements the `Provider` interface (`docs/ARCHITECTURE.md`). Adding a backend = one new package + registration, no edits to the core publish flow. Keep it that easy.
- **CLI command code stays thin.** cobra commands parse flags and delegate; all real logic lives in provider packages and pure helpers with unit tests.
- **mise tasks live inline in `mise.toml`** (`[tasks.build]`, `[tasks.check]`, …). Standalone scripts go in `mise-tasks/` (currently just `mise-tasks/setup`) and are **Nushell** (`#!/usr/bin/env nu`); validate with `nu-check` after editing.
- **Conventional Commits**; **never commit without operator review**.

## Commands (via mise — always use these, not raw go)

| Command | What |
|---|---|
| `mise install` | install toolchain (go 1.26, nushell, golangci-lint, gofumpt, goreleaser) |
| `mise run setup` | download deps + install pre-commit hook |
| `mise run build` | build `bin/htmlup` |
| `mise run check` | fmt + vet + lint + test (= pre-commit) |
| `mise run fmt` | gofumpt the tree |
| `mise run test` | `go test ./...` |
| `mise run tidy` | `go mod tidy` |

## Layout

| Path | Purpose |
|---|---|
| `cmd/htmlup/` | binary entrypoint + cobra command wiring |
| `internal/fsutil/` | `ResolveFS` helper (file/dir → `fs.FS`) |
| `internal/provider/` | `Provider` interface + registry |
| `internal/provider/github/` | GitHub Pages backend (`go-github`); includes `github setup` (`setup.go`) to bootstrap a target repo |
| `internal/provider/s3/` | S3 backend (`aws-sdk-go-v2`) |
| `docs/ARCHITECTURE.md` | design reference (read first) |
| `.claude-plugin/marketplace.json` | plugin marketplace manifest |
| `plugins/htmlup/` | the published Claude skill plugin |
| `mise-tasks/` | Nushell automation |

## Distribution

GoReleaser builds static binaries for `linux`/`darwin` × `amd64`/`arm64` (no Windows). Releases are cut from `v*` tags via `release.yaml`. Each release publishes tar.gz archives (binary + LICENSE + README) and standalone binaries.

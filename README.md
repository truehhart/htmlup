# htmlup 🐊

Coding agents like Claude Code often produce standalone HTML — a dashboard, a report, a quick page. Sharing that as a file is awkward: you have to send it around, and links and assets break once it leaves your machine.

`htmlup` publishes static HTML to a public URL instead. Point it at a file or a directory and it uploads the contents and prints a link per file — no server to run, no hosting account to set up.

**See it live:** [htmlup.truehhart.com](https://htmlup.truehhart.com) — this project's own landing page, published with `htmlup` itself.

Two backends ship today:

- **GitHub Pages** — pushes your files to a repo and lets GitHub Pages serve them.
- **S3** — uploads objects to a bucket, exposed via CloudFront (a built-in HTTP server may come later).

`htmlup` does **no lifecycle management** of what it uploads — it publishes and exits. For GitHub Pages, `htmlup github setup` can install an opt-in cron GitHub Actions workflow in the target repo to expire old pages automatically (see [GitHub Pages cleanup](#github-pages-cleanup) and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)).

## Stack

Go 1.26 · [cobra](https://github.com/spf13/cobra) CLI · [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2) (S3) · [`go-github`](https://github.com/google/go-github) (GitHub Pages) · [GoReleaser](https://goreleaser.com) multi-arch builds · [mise](https://mise.jdx.dev) toolchain · Nushell scripts.

Architecture & command reference: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Agent conventions: [`CLAUDE.md`](CLAUDE.md).

> **Status:** working software, early days. The GitHub Pages and S3 publish flows and `github setup` are implemented; expect rough edges and an evolving feature set.

## Install

[Homebrew](https://brew.sh) (macOS and Linux):

```sh
brew install truehhart/tap/htmlup
```

Otherwise grab a prebuilt binary or `.tar.gz` for your platform from the
[releases page](https://github.com/truehhart/htmlup/releases) (see
[Verifying releases](#verifying-releases)), or build from source below.

## Getting started

> Building from source — for using the released CLI, see [Install](#install).

Prerequisite: [mise](https://mise.jdx.dev) installed.

```sh
mise install        # go 1.26, nushell, golangci-lint, gofumpt, goreleaser
mise run setup      # download deps + install pre-commit hook
mise run build      # produces ./bin/htmlup
mise run check      # fmt + vet + lint + test (also runs on pre-commit)
```

## Usage

Each provider is a top-level subcommand; its flags are scoped to that command. `<path>` is a single `.html` file or a directory (uploaded recursively, relative structure preserved).

By default `github publish` targets wherever GitHub Pages already serves from (its branch + source path are auto-detected), so you usually don't pass `--branch`/`--dir`. Set them explicitly — or pass `--no-auto` — for manual targeting (falls back to `gh-pages` when Pages isn't set up yet). If the target has a `CNAME` file, `publish` reads it to report the custom-domain URL and leaves it in place; it never writes one (use `github setup --cname` to configure a custom domain).

```sh
# GitHub Pages — branch/dir auto-detected from the repo's Pages settings
htmlup github publish ./site --repo owner/repo [--no-auto --branch gh-pages --dir docs]

# S3 (exposed via CloudFront)
htmlup s3 publish ./site --bucket my-bucket [--prefix path/] [--region us-east-1]
```

Each `publish` command also accepts `--dry-run` (enumerate what would be uploaded and the resulting URLs, write nothing) and `-v/--verbose` (per-file progress). On success the command prints a public URL per file, one per line.

### GitHub Pages cleanup

`htmlup github setup` bootstraps a repo for use with `htmlup` in one shot:

```sh
htmlup github setup --repo owner/name [--branch gh-pages] [--ttl-days 30] [--cron "0 3 * * 0"] [--cname example.com] [--exclude staging,*.keep] [--dry-run] [-v]
```

It:

1. Publishes a generated hello-world `index.html` to the Pages branch.
2. Enables GitHub Pages (branch source, path `/`).
3. Installs an opt-in cron cleanup workflow at `.github/workflows/htmlup-cleanup.yaml` on the repo's **default branch**.

The cleanup workflow runs on the `--cron` schedule (default weekly, Sunday 03:00 UTC) and deletes published top-level entries on the Pages branch older than `--ttl-days` (default 30), based on each entry's last commit date. It never removes `index.html`, `CNAME`, `.nojekyll`, or `.github`; pass `--exclude` (repeatable or comma-separated glob patterns, e.g. `--exclude staging,*.keep`) to protect more entries. Removals are recorded as a GitHub-signed (Verified) commit created via the API, so no signing keys are needed in your repo.

## Authentication

`htmlup` never stores credentials — each SDK uses its own standard mechanism:

- **GitHub** — `GITHUB_TOKEN` / `GH_TOKEN`, or the token already configured by the [`gh`](https://cli.github.com) CLI.
- **AWS** — the default credential chain: env vars, `~/.aws/credentials`, SSO, or instance/role credentials.

## Claude skill marketplace

This repo doubles as a [Claude Code plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugins). It publishes the **`htmlup`** skill, which teaches Claude how to drive the CLI.

```sh
# In Claude Code
/plugin marketplace add truehhart/htmlup
/plugin install htmlup@htmlup
```

Marketplace manifest: [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json). Skill source: [`plugins/htmlup/`](plugins/htmlup/).

## Verifying releases

Every release publishes a `SHA256SUMS` file signed with both [Sigstore cosign](https://docs.sigstore.dev/) (keyless) and GPG.

**GPG fingerprint:** `5B91 15D2 57D7 B6FB 65FF  FCA7 DE4C 8787 683F EE7E`

### 1. Verify checksums

```sh
# Download the binary and SHA256SUMS from the release page, then:
sha256sum --check htmlup_*_SHA256SUMS        # Linux
shasum -a 256 --check htmlup_*_SHA256SUMS    # macOS
```

### 2. Verify cosign signature (keyless)

```sh
cosign verify-blob \
  --certificate htmlup_*_SHA256SUMS.pem \
  --signature htmlup_*_SHA256SUMS.sig \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp 'github\.com/truehhart/htmlup' \
  htmlup_*_SHA256SUMS
```

### 3. Verify GPG signature

```sh
gpg --import release/pubkey.asc                                  # one-time: import from this repo
gpg --verify htmlup_*_SHA256SUMS.gpgsig htmlup_*_SHA256SUMS
```

## License

[MIT](LICENSE) © Dmitrii Parshenkov

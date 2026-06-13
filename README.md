# htmlup

Publish HTML pages to the public web with one command. `htmlup` uploads a file or a directory of static HTML and hands you a public URL — no servers to manage.

Two backends ship in the MVP:

- **GitHub Pages** — pushes your files to a repo and lets GitHub Pages serve them.
- **S3** — uploads objects to a bucket, exposed via CloudFront (a built-in HTTP server may come later).

`htmlup` does **no lifecycle management** of what it uploads — it publishes and exits. For GitHub Pages you can optionally enable a cron GitHub Action in the target repo to expire old pages (see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)).

## Stack

Go 1.26 · [cobra](https://github.com/spf13/cobra) CLI · [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2) (S3) · [`go-github`](https://github.com/google/go-github) (GitHub Pages) · [GoReleaser](https://goreleaser.com) multi-arch builds · [mise](https://mise.jdx.dev) toolchain · Nushell scripts.

Architecture & command reference: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Agent conventions: [`CLAUDE.md`](CLAUDE.md).

> **Status:** scaffolding. The CLI is under active development; the command surface below is the design target.

## Getting started

Prerequisite: [mise](https://mise.jdx.dev) installed.

```sh
mise install        # go 1.26, nushell, golangci-lint, gofumpt, goreleaser
mise run setup      # download deps + install pre-commit hook
mise run build      # produces ./bin/htmlup
mise run check      # fmt + vet + lint + test (also runs on pre-commit)
```

## Usage (design target)

```sh
# GitHub Pages
htmlup publish ./site --to github --repo owner/repo [--branch gh-pages] [--dir docs] [--cname example.com]

# S3 (exposed via CloudFront)
htmlup publish ./site --to s3 --bucket my-bucket [--prefix path/] [--region us-east-1]
```

Global flags: `--dry-run` (show what would be uploaded), `-v/--verbose`.

## Authentication

`htmlup` never stores credentials — each SDK uses its own standard mechanism:

- **GitHub** — `GITHUB_TOKEN` / `GH_TOKEN`, or the token already configured by the [`gh`](https://cli.github.com) CLI.
- **AWS** — the default credential chain: env vars, `~/.aws/credentials`, SSO, or instance/role credentials.

## Claude skill marketplace

This repo doubles as a [Claude Code plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugins). It publishes the **`htmlup`** skill, which teaches Claude how to drive the CLI.

```sh
# In Claude Code
/plugin marketplace add truehhart/htmlupclaude
/plugin install htmlup@htmlupclaude
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
  --certificate-identity-regexp 'github\.com/truehhart/htmlupclaude' \
  htmlup_*_SHA256SUMS
```

### 3. Verify GPG signature

```sh
gpg --import release/pubkey.asc                                  # one-time: import from this repo
gpg --verify htmlup_*_SHA256SUMS.gpgsig htmlup_*_SHA256SUMS
```

## License

[MIT](LICENSE) © Dmitrii Parshenkov

<p align="center">
  <a href="https://htmlup.truehhart.com"><img src="docs/public/logo.png" alt="htmlup" width="300" /></a>
</p>

<p align="center"><b>Publish static HTML to a public URL. One command. No server.</b></p>

<p align="center"><i>"I did it before Claude Artifacts became a thing..."</i> 😭</p>

<p align="center">
  <a href="https://github.com/truehhart/htmlup/releases"><img src="https://img.shields.io/github/v/release/truehhart/htmlup?sort=semver" alt="release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="license" /></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="go 1.26" />
</p>

---

Your agent makes HTML — a dashboard, a report, a one-off page. Sharing the file is a pain, and the links break the moment it leaves your machine.

`htmlup` turns it into a link:

```console
$ htmlup publish ./report.html
https://you.github.io/pages/report.html
```

It pushes your files to a backend you already have and prints the public URL. Two ship today — **GitHub Pages** (served from a repo) and **S3** (fronted by CloudFront) — and adding more is a single provider package. No server to run, no hosting account to sign up for.

## Install

```sh
brew tap truehhart/tap
brew trust truehhart/tap   # one-time: Homebrew 6+ requires trusting non-official taps
brew install --cask htmlup
```

No Homebrew? Grab a [prebuilt binary](https://github.com/truehhart/htmlup/releases) ([verify it](#verifying-releases)) or [build from source](#build-from-source).

## Use

```sh
htmlup publish ./site                              # your default profile
htmlup publish github ./site --repo owner/repo     # GitHub Pages
htmlup publish s3 ./site --bucket my-bucket        # S3 (via CloudFront)
```

Pass a file or a directory; directories upload recursively. URLs print to **stdout**, one per line — `htmlup … > urls.txt` captures just the links. Add `--dry-run` to preview, `-v` for progress.

GitHub Pages targets are auto-detected from the repo's Pages settings, so you rarely need `--branch`/`--dir`. `htmlup` only ever uploads — it never deletes; [`setup github`](#github-pages-setup--cleanup) installs an opt-in cron workflow to expire old pages.

## Auth

No credentials stored — each SDK uses its own standard mechanism:

- **GitHub** — `GITHUB_TOKEN` / `GH_TOKEN`, or whatever the [`gh`](https://cli.github.com) CLI already has.
- **AWS** — the default credential chain (env vars, `~/.aws/credentials`, SSO, instance role).

## Claude skill

This repo is also a [Claude Code plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugins) — install the skill and Claude can drive the CLI for you:

```sh
/plugin marketplace add truehhart/htmlup
/plugin install htmlup@htmlup
```

<details>
<summary><b>GitHub Pages setup &amp; cleanup</b></summary>

`htmlup setup github` bootstraps a repo in one shot:

```sh
htmlup setup github --repo owner/name [--branch gh-pages] [--ttl-days 30] [--cron "0 3 * * 0"] [--cname example.com] [--exclude staging,*.keep] [--dry-run] [-v]
```

It (1) publishes a generated hello-world `index.html` to the Pages branch, (2) enables GitHub Pages, and (3) installs a cron cleanup workflow at `.github/workflows/htmlup-cleanup.yaml` on the default branch.

The workflow runs on the `--cron` schedule (default weekly, Sunday 03:00 UTC) and deletes top-level entries older than `--ttl-days` (default 30), by each entry's last commit date. It never removes `index.html`, `CNAME`, `.nojekyll`, or `.github`; use `--exclude` to protect more. Removals are GitHub-signed (Verified) commits made via the API — no signing keys needed in your repo.

An existing `CNAME` is read to report the custom-domain URL and left in place — `htmlup` never writes one (use `setup github --cname` for that). Pass `--no-auto --branch … --dir …` to target manually.

</details>

<details>
<summary><b>Build from source</b></summary>

Prerequisite: [mise](https://mise.jdx.dev).

```sh
mise install        # go 1.26, nushell, golangci-lint, gofumpt, goreleaser
mise run setup      # download deps + install pre-commit hook
mise run build      # produces ./bin/htmlup
mise run check      # fmt + vet + lint + test (also runs on pre-commit)
```

</details>

<details>
<summary><b>Verifying releases</b></summary>

Every release publishes a `SHA256SUMS` file signed with both [Sigstore cosign](https://docs.sigstore.dev/) (keyless) and GPG.

**GPG fingerprint:** `5B91 15D2 57D7 B6FB 65FF  FCA7 DE4C 8787 683F EE7E`

```sh
# 1. Verify checksums (download the binary + SHA256SUMS first)
sha256sum --check htmlup_*_SHA256SUMS        # Linux
shasum -a 256 --check htmlup_*_SHA256SUMS    # macOS

# 2. Verify cosign signature (keyless)
cosign verify-blob \
  --certificate htmlup_*_SHA256SUMS.pem \
  --signature htmlup_*_SHA256SUMS.sig \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp 'github\.com/truehhart/htmlup' \
  htmlup_*_SHA256SUMS

# 3. Verify GPG signature
gpg --import release/pubkey.asc
gpg --verify htmlup_*_SHA256SUMS.gpgsig htmlup_*_SHA256SUMS
```

</details>

---

<sub>Working software, early days — expect rough edges. Design notes: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) · [MIT](LICENSE) © Dmitrii Parshenkov</sub>

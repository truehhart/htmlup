---
name: htmlup
description: Publish HTML pages to the public web with the htmlup CLI — uploads a file or static-site directory to GitHub Pages or an S3 bucket and returns a public URL. Use when the user wants to publish, host, or share an HTML page or static site, or mentions GitHub Pages / S3 hosting via htmlup.
---

# Publishing HTML with htmlup

`htmlup` takes local HTML (a single file or a directory of static assets) and makes it publicly reachable. It is stateless: it uploads and exits, and does no lifecycle management of what it publishes.

> **Status:** the CLI is under active development. This skill documents the design-target command surface; verify against `htmlup --help` before relying on exact flags.

## Decide the backend

- **GitHub Pages** — the user has (or wants to use) a GitHub repo served by Pages. Best for docs/demos tied to a repo.
- **S3** — the user wants object storage, typically fronted by CloudFront for delivery.

If the user hasn't said, ask which backend they want rather than guessing.

## Authentication (do not handle credentials yourself)

`htmlup` relies on each SDK's standard credential resolution:

- **GitHub** — needs `GITHUB_TOKEN` / `GH_TOKEN`, or a logged-in `gh` CLI (`gh auth login`). If publishing fails with an auth error, point the user there.
- **AWS** — uses the default credential chain (env vars, `~/.aws/credentials`, SSO, instance/role). If it fails, suggest `aws configure` / `aws sso login`.

## Commands

GitHub Pages:

```sh
htmlup publish <path> --to github --repo owner/repo [--branch gh-pages] [--dir docs] [--cname example.com]
```

S3 (exposed via CloudFront):

```sh
htmlup publish <path> --to s3 --bucket my-bucket [--prefix path/] [--region us-east-1]
```

`<path>` is a single `.html` file or a directory (uploaded recursively, relative structure preserved).

Useful global flags:

- `--dry-run` — preview what would be uploaded and the resulting URL without writing anything. Prefer this first when the target is unfamiliar.
- `-v, --verbose` — per-file progress.

## Workflow

1. Confirm the local path exists and what should be published (single page vs. whole directory).
2. Pick the backend (ask if unclear) and gather its required flags (`--repo` for GitHub, `--bucket` for S3).
3. Run with `--dry-run` first to confirm the file set and target URL.
4. Run for real; surface the printed public URL back to the user.
5. On auth errors, direct the user to the relevant login step above — never attempt to supply or store credentials.

## Notes

- `htmlup` does not expire or clean up old uploads. For GitHub Pages, mention the optional cron GitHub Action template (installed in the *target* repo) if the user asks about cleanup.
- Windows is not supported; binaries are `linux`/`darwin` on `amd64`/`arm64`.

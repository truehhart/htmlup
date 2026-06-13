# Architecture — htmlup

> Design reference. This document is the source of truth for the command surface, the provider abstraction, the auth model, and the GitHub Pages lifecycle story. Implementation lands in later passes; nothing here is built yet.

## 1. What it is

`htmlup` is a single-purpose CLI: take local HTML (a file or a directory tree) and make it publicly reachable. It is stateless and does **no lifecycle management** — every invocation uploads and exits. There is no daemon, no local database, no tracking of what was published.

## 2. Command surface

```
htmlup publish <path> --to <github|s3> [provider flags] [global flags]
```

- `<path>` — a single `.html` file or a directory of static assets. Directories upload recursively, preserving relative structure.
- `--to` — selects the provider. The flag value is the provider's registered name; adding a provider adds a value here automatically (see §3).

**Global flags**

| Flag | Purpose |
|---|---|
| `--dry-run` | enumerate what would be uploaded and the resulting URL; perform no writes |
| `-v, --verbose` | per-file progress and SDK-level detail |

**GitHub Pages provider**

| Flag | Required | Purpose |
|---|---|---|
| `--repo owner/name` | yes | target repository |
| `--branch` | no (default `gh-pages`) | branch to push to |
| `--dir` | no | subdirectory within the branch (Pages "folder" source) |
| `--cname` | no | write a `CNAME` file for a custom domain |

**S3 provider**

| Flag | Required | Purpose |
|---|---|---|
| `--bucket` | yes | target bucket |
| `--prefix` | no | key prefix (logical folder) |
| `--region` | no | overrides region from the AWS config chain |

On success the command prints the public URL (Pages URL, or the S3/CloudFront URL the operator wires up).

## 3. Provider abstraction

Backends are pluggable behind one interface so a contributor can add a target without touching the publish flow. Sketch:

```go
// internal/provider
type Target struct {
    Files   fs.FS   // resolved local files, relative paths preserved
    DryRun  bool
    Verbose bool
}

type Result struct {
    URL string // public URL of the published content
}

type Provider interface {
    Name() string                                  // registry key, also the --to value
    BindFlags(*pflag.FlagSet)                       // provider-specific flags
    Validate() error                                // post-parse flag validation
    Publish(ctx context.Context, t Target) (Result, error)
}
```

Providers self-register into a registry (`init()` → `provider.Register(...)`). `cmd/htmlup` discovers them generically: the `--to` enum and per-provider flag groups are derived from the registry, never hardcoded in the core command. **Adding a backend = one new package under `internal/provider/<name>/` + registration. No edits to the publish command.**

MVP providers:

- `internal/provider/github` — uses `go-github`. Commits the file set to the target branch (Git Data API / Contents API), creates the branch if missing, optionally writes `CNAME`. Enables Pages if not already on.
- `internal/provider/s3` — uses `aws-sdk-go-v2`. `PutObject` per file with content-type inferred from extension. No bucket policy / website-config mutation in the MVP (the operator owns exposure via CloudFront).

## 4. Authentication

`htmlup` delegates **entirely** to the official SDKs' standard credential resolution. It does not read, store, prompt for, or cache credentials.

- **GitHub** — `golang.org/x/oauth2` static token sourced from `GITHUB_TOKEN` / `GH_TOKEN`. If unset, fall back to the token the `gh` CLI has stored (via `go-gh`). Missing token → actionable error pointing at `gh auth login`.
- **AWS** — `config.LoadDefaultConfig(ctx)` (env vars → shared config/credentials → SSO → role/instance). `--region` only overrides the resolved region.

## 5. GitHub Pages lifecycle (opt-in, target-repo side)

`htmlup` itself never deletes anything. For users who want pages to expire, we offer an **opt-in cron GitHub Action installed in the target repo** (not in this repo, not run by the CLI). It periodically removes published directories older than a configured TTL based on commit metadata. This is a documented template the user copies into their Pages repo; design and template live in a later pass. Out of scope for the MVP CLI.

## 6. S3 exposure

The MVP only uploads objects. Public exposure is the operator's responsibility, expected via **CloudFront** in front of the bucket. A future (non-MVP) option may let `htmlup` run as a simple static HTTP server over a bucket; it is explicitly out of scope now.

## 7. Distribution

GoReleaser produces static binaries for `linux`/`darwin` × `amd64`/`arm64` (no Windows). Tag-triggered GitHub Actions cut releases and publish artifacts. CI is wired by a separate agent; this pass only records the intent.

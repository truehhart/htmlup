# Architecture — htmlup

> Design reference. This document is the source of truth for the command surface, the provider abstraction, the auth model, and the GitHub Pages lifecycle story.

## 1. What it is

`htmlup` is a single-purpose CLI: take local HTML (a file or a directory tree) and make it publicly reachable. It is stateless and does **no lifecycle management** — every invocation uploads and exits. There is no daemon, no local database, no tracking of what was published.

## 2. Command surface

```
htmlup <provider> publish <path> [flags]
```

Each provider is a top-level subcommand. Provider-specific flags are scoped to the provider's `publish` command — no flag collisions are possible.

- `<path>` — a single `.html` file or a directory of static assets. Directories upload recursively, preserving relative structure.

**Common flags** (on every provider's `publish` command)

| Flag | Purpose |
|---|---|
| `--dry-run` | enumerate what would be uploaded and the resulting URLs; perform no writes |
| `-v, --verbose` | per-file progress and SDK-level detail |

**`htmlup github publish`**

| Flag | Required | Purpose |
|---|---|---|
| `--repo owner/name` | yes | target repository |
| `--branch` | no | branch to push to (default: auto-detected from the repo's Pages source, else `gh-pages`) |
| `--dir` | no | subdirectory within the branch (default: auto-detected from the Pages source path) |
| `--no-auto` | no | disable Pages auto-detection; use `--branch`/`--dir` as given |

By default `publish` targets wherever GitHub Pages already serves from: it reads the repo's Pages source (branch + path) and pushes there, so a plain `htmlup github publish ./site --repo owner/name` lands in the right place. Setting `--branch`/`--dir` explicitly (or `--no-auto`) switches to manual targeting; if Pages is off or built from a workflow, it falls back to `gh-pages`.

`publish` does not write a `CNAME` — it only **reads** an existing one at the target's source root to report the custom-domain URL, and (because it merges onto the branch's tree) leaves it untouched. Configuring a custom domain is `htmlup github setup --cname`'s job.

**`htmlup s3 publish`**

| Flag | Required | Purpose |
|---|---|---|
| `--bucket` | yes | target bucket |
| `--prefix` | no | key prefix (logical folder) |
| `--region` | no | overrides region from the AWS config chain |

On success the command prints a public URL per published file, one per line, to **stdout** (Pages URLs, or the S3/CloudFront URLs the operator wires up). Human status — progress, dry-run previews, warnings, next-step hints — goes to **stderr**, so `htmlup … > urls.txt` captures only the machine-readable list. See [§7 Output & interaction](#7-output--interaction).

## 3. Provider abstraction

Backends are pluggable behind one interface so a contributor can add a target without touching the publish flow. Sketch:

```go
// internal/provider
type Target struct {
    Files   fs.FS       // resolved local files, relative paths preserved
    DryRun  bool
    Verbose bool
    UI      *ui.Output  // sink for all human status; never printed to os.Stderr directly
}

type Result struct {
    URLs []string // public URL of each published file, in upload order
}

type Provider interface {
    Name() string              // registry key, also the subcommand name
    Command() *cobra.Command   // returns the provider's subcommand tree
}
```

A provider emits every human-facing line through `Target.UI` (or the `*ui.Output` passed to `Publish`); the command layer writes the resulting `Result.URLs` to stdout. No provider touches `os.Stdout`/`os.Stderr` — see [§7](#7-output--interaction).

Providers self-register into a registry (`init()` → `provider.Register(...)`). `cmd/htmlup` discovers them generically: each provider's `Command()` is added as a child of the root cobra command. **Adding a backend = one new package under `internal/provider/<name>/` + registration. No edits to `cmd/htmlup/`.**

MVP providers:

- `internal/provider/github` — uses `go-github`. Commits the file set to the target branch (Git Data API / Contents API), creates the branch if missing. `publish` reads an existing `CNAME` for the URL; `setup --cname` writes one. Enables Pages if not already on.
- `internal/provider/s3` — uses `aws-sdk-go-v2`. `PutObject` per file with content-type inferred from extension. No bucket policy / website-config mutation in the MVP (the operator owns exposure via CloudFront).

## 4. Authentication

`htmlup` delegates **entirely** to the official SDKs' standard credential resolution. It does not read, store, prompt for, or cache credentials.

- **GitHub** — `golang.org/x/oauth2` static token sourced from `GITHUB_TOKEN` / `GH_TOKEN`. If unset, fall back to the token the `gh` CLI has stored (via `go-gh`). Missing token → actionable error pointing at `gh auth login`.
- **AWS** — `config.LoadDefaultConfig(ctx)` (env vars → shared config/credentials → SSO → role/instance). `--region` only overrides the resolved region.

## 5. GitHub Pages lifecycle (opt-in, target-repo side)

The publish path itself never deletes anything. For users who want pages to expire, we offer an **opt-in cron GitHub Actions workflow installed in the target repo** (not in this repo, not run by the CLI at publish time). It periodically removes published top-level entries older than a configured TTL based on each entry's last-commit date.

`htmlup github setup --repo owner/name` installs this workflow in one shot. It:

1. publishes a generated hello-world `index.html` to the Pages branch (default `gh-pages`), plus a `CNAME` file when `--cname` is given,
2. enables GitHub Pages (branch source, path `/`), and
3. commits `.github/workflows/htmlup-cleanup.yaml` to the target repo's **default branch**.

The workflow runs on `--cron` (default `0 3 * * 0`, weekly Sun 03:00 UTC) plus `workflow_dispatch`, holds `contents: write`, checks out the Pages branch, and deletes top-level entries whose last commit is older than `--ttl-days` (default 30). It never removes `index.html`, `CNAME`, `.nojekyll`, or `.github`; `--exclude` (repeatable / comma-separated glob patterns) adds more entries to that protected list. The removals are recorded as a **GitHub-signed commit** created through the API (`createCommitOnBranch`) using the workflow token — so the commit shows as Verified and no GPG keys live in the target repo. The CLI commits the workflow once and exits; all deletion happens later, on schedule, inside the target repo — the publish path remains stateless and lifecycle-free.

## 6. S3 exposure

The MVP only uploads objects. Public exposure is the operator's responsibility, expected via **CloudFront** in front of the bucket. A future (non-MVP) option may let `htmlup` run as a simple static HTTP server over a bucket; it is explicitly out of scope now.

## 7. Output & interaction

All user-facing text is owned by one package, `internal/ui`, so the CLI speaks with a single voice and the machine/human split stays intact regardless of which command or backend produced the output. It is built on the **charmbracelet** stack — [`lipgloss`](https://github.com/charmbracelet/lipgloss) for status styling and [`huh`](https://github.com/charmbracelet/huh) for interactive prompts — both wrapped so no command or provider imports them directly. Nothing else prints directly either: a guard test (`cmd/htmlup/contract_test.go`) fails the build on a stray `fmt.Print*`, `fmt.Fprintf(os.Stderr, …)`, or `cmd.Print*` outside `internal/ui`.

- **`ui.Output`** routes the two streams. **stdout** carries only machine-readable results — the published URLs (`URLs`), a config dump (`Plain`), the version (`Result`) — so piping stays clean. **stderr** carries human status, styled with lipgloss, via typed helpers: `Info` (neutral), `Success` (green, led by `✓`), `Warn` (yellow `warning:`), `Error` (red `error:` — the one place a returned error is printed, at the top level), `DryRun`/`Next` (cyan labels), `Progress` (faint, verbose-only), and `Detail` (indented continuation, e.g. each previewed URL). Commands build one with `ui.Auto()` and pass it into provider code through `Target.UI` / the `*ui.Output` argument to `Publish`.
- **`ui.Prompter`** is the only interactive surface: `Line` (free text + default + validation), `Select` (pick from a list), `Confirm` (y/n). On an interactive color terminal it renders a polished huh form; off a TTY or under `NO_COLOR` it falls back to a plain line-based reader with identical behavior (this is also the path unit tests and pipes drive). The `config init` wizard and `github setup`'s repoint confirmation both go through it; `Interactive()` reports whether input is a TTY so an unattended run declines optional prompts instead of blocking. Cancelling a prompt (Ctrl+C) surfaces `ui.ErrAborted`; the root command treats it as a clean cancel — no error line, exit code 130.
- **Styling is policy, resolved once.** Color (and the leading glyphs) is decorative only and auto-disabled off a TTY and whenever [`NO_COLOR`](https://no-color.org/) is set — meaning always lives in the words, so the no-color path drops every glyph to nothing and renders plain text. `NO_COLOR` also drops huh back to the plain prompts, so honoring it never costs functionality. There is intentionally no `--color` flag: TTY detection plus `NO_COLOR` covers the cases that matter and keeps the surface small.

Adding a backend therefore touches no output code: implement `Provider`, emit through the `ui.Output` you're handed, and the contract holds automatically.

## 8. Distribution

GoReleaser produces static binaries for `linux`/`darwin` × `amd64`/`arm64` (no Windows). Releases are cut by the **manually-triggered `release.yaml`** workflow: it takes a `version` input (without the leading `v`), validates it, and runs the check suite. Only then does publish **create and push the `v<version>` tag and check it out**, so artifacts are built *from the tag*, not from whatever the branch happens to be. **GoReleaser** then builds, signs (cosign keyless + GPG), and publishes the release: tar.gz archives, standalone binaries, and a signed `SHA256SUMS`, with notes from the commit log (`docs:`/`chore:` filtered out). `release.mode: replace` makes a retried release land cleanly, and pushing the tag only after checks pass avoids leaving an orphan tag behind a failed run.

Versions are immutable: re-running `release.yaml` for a `version` whose tag already exists will not move the tag (it stays at the original commit), so a re-run only makes sense for the same commit. To ship new content, bump the version.

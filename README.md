# gh-threads

`gh-threads` is a Go implementation of the original `threads` CLI. It shells out to your authenticated `gh` client to fetch, cache, and render pull-request review threads either as JSON or an ANSI-rich summary/interactive view.

## Install as a GitHub CLI extension

You get the intended experience by installing the extension directly from GitHub:

```bash
gh extension install VRTFinland/github-pr-threads
```

After installation the command is available as `gh threads …`. Update anytime with:

```bash
gh extension upgrade VRTFinland/github-pr-threads
```

> Prerequisites: `gh` must already be authenticated (`gh auth status`) and Go 1.25+ is needed only if you plan on building from source.

## Usage

```bash
gh threads 13533 --repo owner/name --format summary --show-diff
```

Key flags:

- `--format summary|json`
- `--status all|resolved|unresolved`
- `--show-diff/--hide-diff`
- `--author <github_login>`
- `--no-colour` / `--no-markdown`
- `--refresh-cache` to bypass persisted data under `$XDG_CACHE_HOME/threads`

### Interactive mode

Add `--interactive` (or `-i`) to launch the Bubble Tea UI without re-running GraphQL queries:

```bash
gh threads 13533 --repo owner/name --interactive
```

Navigation cheatsheet:

- `↑/↓` or `k/j` – move between threads.
- `←/→` or `h/l` – move between comments inside the detail pane.
- `Enter`/`Space` – expand or collapse the detail pane.
- `d` – toggle snippet vs. raw diff.
- `r` – reply to the selected comment (`Esc` cancels).
- `s` – open the status filter picker; `S` toggles resolve/unresolve.
- `/` – text filter; `a` – author filter; `f` – cycle status filters.
- `R` – refresh threads from GitHub; `q` or `Ctrl+C` – exit.

## Local development

Clone the repository and work from its root:

```bash
go run ./cmd/gh-threads --repo owner/name 13533 --format summary
go test ./...
go build ./cmd/gh-threads
```

The resulting `gh-threads` binary can be copied anywhere in your `PATH` (GitHub CLI auto-detects it as `gh threads`) or registered locally via:

```bash
gh extension install .
```

Rendering relies on Glamour, Lip Gloss, and Bubble Tea; any changes to colouring should go through the helpers under `internal/render` and `internal/interactive`.

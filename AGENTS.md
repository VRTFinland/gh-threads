# Repository Guidelines

## Project Structure & Module Organization
`cmd/gh-threads/main.go` bootstraps the CLI and delegates to `internal/app`, which owns flag parsing, repo/PR detection, caching switches, and the summary vs. interactive execution paths. All GitHub I/O runs through `internal/ghcli`, `internal/threads` carries the service layer, cache, and payload builders, and `internal/render` formats JSON and Glamour-powered summaries. The interactive TUI lives in `internal/interactive` (Bubble Tea, Lip Gloss, Glamour) with view logic split between `model.go` and `view.go`. Helpers that touch the local checkout (`internal/gitlocal`) or fetch remote file contents (`internal/gitremote`) are reusable utilities—keep new packages under `internal/` unless they are meant to be public.

## Build, Test, and Development Commands
- `go run ./cmd/gh-threads --repo owner/name 13533 --format summary --show-diff` — quickest way to exercise the CLI; swap flags to inspect JSON, colour, markdown, or cache invalidation behaviours.
- `go run ./cmd/gh-threads --interactive --repo owner/name 13533` — smoke-test the Bubble Tea UI and keyboard handling (refer to README for bindings).
- `go test ./...` — run the Go unit tests (covers cache/service logic plus interactive helpers); keep suites hermetic by stubbing `ghcli` interactions.
- `go build ./cmd/gh-threads` — ensure binaries embed cleanly before publishing or installing as a `gh` extension.

## Coding Style & Naming Conventions
All code should be `gofmt`/`goimports` clean with idiomatic naming: exported types and constructors use `PascalCase`, private helpers use `camelCase`, and package names stay `lowercase`. Prefer small, pure helpers for filtering/sorting data (see `internal/threads/filter.go`) and keep context-aware methods taking a `context.Context`. Interactive UI helpers should be prefixed with their function (`renderThreadList`, `renderDetailBlock`) and rendering logic should flow through Glamour/Lip Gloss utilities for consistent styling. Avoid pulling new dependencies unless every extension user benefits.

## Testing Guidelines
Use the existing Go test layout under `internal/`: e.g., extend `internal/threads/service_test.go` when touching cache/service behaviour, and `internal/interactive/*_test.go` for model or rendering helpers. Prefer table-driven tests, stub the `ghcli.Client` interface to avoid network calls, and keep interactive tests deterministic by bypassing Bubble Tea event loops. Run `go test ./...` locally before pushing; if you rely on manual CLI checks (`go run ./cmd/gh-threads ...`), summarize the command/output in the PR.

## Commit & Pull Request Guidelines
Commits stay short and imperative (e.g., "Add cache busting flag") with diffs scoped to a single concern. PR descriptions must explain the problem, approach, and list validation commands (`go test ./...`, any `go run ./cmd/gh-threads ...` scenarios, plus manual interactive runs when applicable). Link the related issue, attach screenshots or transcripts for UI tweaks, and mention GitHub API rate-limit considerations if you touched polling/caching.

## Security & Configuration Tips
The Go binary shells out to `gh`, so ensure `gh auth status` succeeds before development or tests. Never commit built binaries, tokens, cached GraphQL payloads, or logs that could expose private repositories; `internal/threads/cache.go` already persists per-PR data under the user cache directory, so document any manual cache manipulation steps in the PR instead of checking artefacts in. Prefer environment variables or CLI flags for configuration, and keep README notes updated so other agents can reproduce your setup quickly.

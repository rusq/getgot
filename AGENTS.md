# Repository Guidelines

## Project Structure & Module Organization

This is a small Go command-line daemon (`github.com/rusq/getgot`). Production
code currently lives in `main.go`; module metadata is in `go.mod` and `go.sum`.
`README.md` is the user-facing overview, while `BUGS.md` records audited defects
and the getgot-side repair plan against gotsr v0.2.0. There are currently no test files
or bundled assets. The daemon dependency is also checked out at `../gotsr`; read
its `AGENTS.md` before changing lifecycle behavior there.

## Build, Test, and Development Commands

- `go build ./...` — compile all packages.
- `go run . -names="example" -every=5s` — run locally. Use only a unique,
  disposable process name; defaults include real system utilities.
- `go test -count=1 ./...` — run tests without cached results.
- `go test -race ./...` — check test-covered concurrency paths.
- `go vet ./...` — run standard static analysis.
- `staticcheck ./...` and `govulncheck ./...` — run when installed.
- `git diff --check` — catch whitespace errors before review.

Do not launch the daemon against its default `jamf,Nudge,du` list during routine
validation. Prefer isolated helper processes for lifecycle tests.

## Coding Style & Naming Conventions

Use Go 1.27-compatible, idiomatic code and format touched files with `gofmt`.
Keep functions small, return contextual errors with `%w`, and use `errors.Is`
for sentinel checks. Follow standard Go naming: exported identifiers use
CamelCase, unexported helpers use lowerCamelCase, and common initialisms remain
capitalized (`PID`, `TSR`). Avoid new dependencies unless they materially
simplify a tested design.

## Testing Guidelines

Add table-driven tests in `*_test.go` files, named `TestFunction` with descriptive
`t.Run` cases. Process-management fixes require subprocess regression tests that
use `t.TempDir`, disposable helper binaries, bounded timeouts, and reliable
cleanup. Never signal unrelated host processes. Cover partial kill failures,
logging, parsing, stale state, repeated starts, and clean termination as relevant.

## Commit & Pull Request Guidelines

History uses short, imperative summaries such as `add Nudge` and `cancel context
on exit`; keep commits similarly focused. Pull requests should explain behavior
and risk, link the relevant `BUGS.md` finding or issue, list exact validation
commands, and call out platform coverage. Screenshots are unnecessary unless a
future UI is added. Keep `getgot` and `gotsr` changes in their respective
repositories and identify the dependency commit or release joining them.

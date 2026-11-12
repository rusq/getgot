# Audit findings and fix plan

Audit date: 2026-11-11, revised 2026-11-12

Audited checkout: `getgot` `master` at `455fc74` (`v0.1.1`), which depends on
`github.com/rusq/gotsr v0.1.0`.

Target dependency: `github.com/rusq/gotsr v0.2.0` (`26ef91e`, checked out at
`../gotsr`). It identifies the running daemon on POSIX by an `fcntl` lock on
`<pidfile>.lock`. The earlier control-endpoint design (gotsr#2) was abandoned,
and so was the getgot branch built on it (`fix/audit-findings`, getgot#2).
Nothing from that branch should be reused unless it is checked against v0.2.0
first.

Findings are ordered by severity. Line numbers refer to `main.go` at `v0.1.1`.

## History

- 2026-11-11: first audit. The two-repository plan produced gotsr#2 and
  getgot#2 (`fix/audit-findings`).
- 2026-11-11: gotsr#2 was dropped as too complex. gotsr#3 (the `fcntl` lock
  design) shipped as gotsr `v0.2.0`.
- 2026-11-12: a review of getgot#2 found that it still pinned a gotsr#2 commit
  (`f63662c`). Its README also described the dropped design (authenticated
  state, manual cleanup of old state). Local getgot `master` was reset to
  `v0.1.1`, the same commit as `origin/master`. The `fix/audit-findings` branch
  is kept for reference, and getgot#2 is still open but superseded by this plan.
- 2026-11-12: this document was revised against gotsr `v0.2.0`.
- 2026-11-12: decided to move getgot to Go 1.27 to match gotsr `v0.2.0`
  (finding 9).
- 2026-11-12: the fix plan was carried out in getgot PRs #3 to #10, one per
  step, each reviewed before merge. Limitations accepted in review are listed
  in the README and in the PR descriptions.

## Status against gotsr v0.2.0

| # | Finding | Where it gets fixed | Status |
|---|---------|---------------------|--------|
| 1 | A second launch kills the running daemon | gotsr v0.2.0: upgrade only | fixed, #3 |
| 2 | A stale PID file can signal an unrelated process | gotsr v0.2.0 on POSIX: upgrade only | fixed, #3 |
| 3 | State depends on the working directory | getgot | fixed, #4 |
| 4 | Simultaneous starts and the startup checks | getgot | fixed, #7 |
| 5 | One failed kill stops the scan, and PIDs can be reused | getgot | fixed, #8 |
| 6 | `-log` misses `slog` output and has a loose default | getgot | fixed, #6 |
| 7 | Process names are parsed loosely | getgot | fixed, #5 |
| 8 | Shutdown races with the main goroutine | getgot | fixed, #9 |
| 9 | gotsr v0.2.0 requires Go 1.27 | decided: getgot moves to Go 1.27 | done, #3 |
| 10 | Windows stop and status are unauthenticated | gotsr (known limitation) | documented in README, #10 |

## Findings

### 1. High: a second launch terminates the running daemon (fixed in gotsr v0.2.0)

`main.go:54` calls `p.IsRunning()`. In gotsr v0.1.0, `isRunning` probes the PID
with `SIGUSR2`, which the daemon does not handle, so the probe kills it.

gotsr v0.2.0 answers `IsRunning` with `F_GETLK` on the lock file and sends no
signal (`../gotsr/tsr_posix.go`, `isRunning`). Its `TestLifecycle` covers this
regression. Upgrading the dependency is enough.

### 2. High: a stale PID file can signal an unrelated process (fixed on POSIX in gotsr v0.2.0)

In gotsr v0.1.0, `IsRunning` and `Terminate` trust the PID written in the PID
file. In v0.2.0, `Terminate` only signals the process that holds the lock. A
stale PID file without a lock holder returns `ErrNotRunning`. On Linux the
signal goes through a pidfd and the lock is checked again after the handle is
obtained, which rules out PID reuse. On Darwin and the BSDs a very small window
remains between that check and the signal. That is accepted upstream.

What getgot must do: put the PID and lock files on a **local filesystem**
(see finding 3). Lock-holder PIDs mean nothing over NFS.

### 3. Medium: singleton and `-stop` depend on the working directory

`main.go:39` calls `gotsr.New()` without `WithPIDFile`. In v0.2.0 the default
`getgot.pid` is made absolute when `New` is called, but it is still relative
to the caller's working directory. Two working directories can therefore run
two daemons, and `-stop` from a third directory finds neither.

Fix: pass `gotsr.WithPIDFile` with a stable, per-user path on a local
filesystem:

- Linux: `$XDG_RUNTIME_DIR/getgot/` when it is set. Otherwise use
  `os.TempDir()/getgot-<uid>/`, after checking that the directory is not a
  symlink, is owned by the current user and has mode `0700`, because `/tmp` is
  shared.
- macOS: `os.TempDir()` (`$TMPDIR`), which is per-user, private and local.
- Do not use `os.UserConfigDir()`, because home directories can be on NFS.

The lock file `getgot.pid.lock` is never removed. This is intentional in gotsr.

### 4. Medium: simultaneous starts and the startup checks

`main.go:54-59` runs `IsRunning` before `TSR`, and runs it again in each of
gotsr's three stage processes. In v0.2.0, `TSR` already checks the lock in the
initial stage and returns `gotsr.ErrAlreadyRunning`. The running stage also
reports `ErrAlreadyRunning` when it loses the lock race.

The problems:

- `main.go:69-71` sends every `TSR` error to `log.Fatal`. If two launchers race,
  the loser exits with status 1 and prints "already running" or "process exited
  during startup" instead of the friendly warning.
- In the running stage, the early `IsRunning` check can return without
  reporting anything. The launcher then sees `errExited` and not
  `ErrAlreadyRunning`.

Fix: drop the separate `IsRunning` check. Call `TSR` and treat
`errors.Is(err, gotsr.ErrAlreadyRunning)` as "getgot already running" with exit
status 0. If a status message is wanted, it can come from the same error.

### 5. Medium: one failed kill stops the scan, and PIDs can be reused

`wipe` returns at the first `FindProcess` or `Kill` error (`main.go:121-127`).
A matching process that cannot be killed hides every later match on every
pass, because the enumeration order is not defined.

Also:

- `main.go:121-128`: a matched process can exit between `ps.Processes()` and
  `Kill`, and its PID can be reused by an unrelated process, which then gets
  `SIGKILL`. On Linux, `os.FindProcess` holds a pidfd, so re-checking the
  executable name after obtaining the handle closes this window. Elsewhere it
  only narrows it.
- The handles are never `Release`d, so each scan leaks a pidfd (Linux) or a
  handle (Windows) until the finalizer runs.

Fix: try every match, log each success and failure, `Release` each handle,
re-check the name after `FindProcess`, and return `errors.Join` of all
failures. Put process listing and finding behind a small interface so tests can
cover partial failure without killing real processes.

### 6. Medium: `-log` misses `slog` output and has a loose default

`main.go:83` redirects only the legacy `log` package. Kill and wipe messages go
through the default `slog` logger (`main.go:104`, `main.go:128`). gotsr
disconnects their stderr in the detached process, so they are lost.

Also:

- The default log is `getgot.log` relative to the working directory, with mode
  `0644` (`main.go:78`).
- If opening the log fails in the daemon (`main.go:79-81`), the daemon calls
  `log.Fatal` *after* gotsr has already told the launcher "OK". The launcher
  prints "starting getgot..." and the daemon dies without a visible error.

Fix: install a `slog` handler on the opened file and set it as the default.
Keep `log` pointed at the same file. Open the log **before** `TSR`, in the
launcher, so that a bad path fails in the foreground. A relative `-log` value
should be made absolute before `TSR`, so that the detached process resolves it
the same way. Use mode `0600`, and use a stable default directory such as
`os.UserCacheDir()/getgot/`. The log does not need to be on a local
filesystem. Write a "started" and a "stopping" line.

### 7. Low: process names are parsed loosely

`strings.Split("", ",")` returns one empty string, so `-names=""` passes the
check at `main.go:64-67`. Names are not trimmed, so `-names="foo, bar"` looks
for `" bar"`.

Also:

- go-ps reports the kernel's command name, which is cut to 15 characters on
  Linux and 16 on Darwin. A longer name never matches, and nothing is logged.
- Nothing stops `-names=getgot` or the getgot executable name from matching.
  The daemon would then `SIGKILL` itself, and skip its shutdown cleanup.

Fix: split, trim, reject empty entries, remove duplicates, and reject names
longer than the platform limit. Never kill `os.Getpid()`. Cover this with a
table-driven test.

### 8. Low: shutdown races with the main goroutine

`main.go:62` cancels the context from gotsr's `AtExit` hook. gotsr runs the
hooks in its signal goroutine, then removes its PID file, releases the lock
and calls `os.Exit(0)`. Meanwhile `supress` returns `context.Canceled`, and
`main.go:84-85` calls `log.Fatal`. That can exit with status 1 before gotsr
finishes. The result is a false "context canceled" fatal line in the log and,
sometimes, a leftover `getgot.pid` file. The leftover file is harmless with
v0.2.0, because the lock is the source of truth.

Fix: treat `context.Canceled` as a normal stop. After `supress` returns, block
or return without `log.Fatal`, and let gotsr finish and exit. Only log real
errors.

### 9. Decided: getgot moves to Go 1.27

`../gotsr/go.mod` declares `go 1.27`. Requiring v0.2.0 makes `go mod tidy`
raise getgot's `go 1.24` directive to `go 1.27`.

Decision (2026-11-12): getgot moves to Go 1.27 and requires gotsr `v0.2.0`
as it is. gotsr does not change. The rule in `AGENTS.md` now says Go
1.27. The `go.mod` directive changes in the same commit as the dependency
update (fix plan step 1).

### 10. Low: Windows stop and status are unauthenticated (gotsr limitation)

gotsr's Windows code is experimental. It does not lock the PID file, and it
answers status and stop on a `127.0.0.1` TCP port, which any local user can
reach. getgot targets macOS first, so record this as a platform limitation in
the README. Do not fix it in getgot.

## Fix plan

Everything happens in getgot. gotsr needs no change.

1. Require `github.com/rusq/gotsr v0.2.0` and run `go mod tidy`. This raises
   the `go` directive to `1.27` (finding 9). Do not commit a `replace`
   directive.
2. Add `statePaths()`, returning the PID file path (local, private, per-user;
   finding 3) and the default log path (finding 6). Pass the PID path through
   `gotsr.WithPIDFile`.
3. Add `parseProcessNames` (finding 7) and run it before `TSR`.
4. Open the log and install the `slog` and `log` output before `TSR`
   (finding 6).
5. Replace the `IsRunning` check with handling of `ErrAlreadyRunning` from
   `TSR` (finding 4).
6. Rework `wipe` (finding 5) behind a narrow process-lister interface.
7. Make shutdown clean (finding 8).
8. Update `README.md`:
   - start, repeated start, `-stop`;
   - where the state and log files are, and that the `.lock` file persists;
   - the name length limit;
   - that matching processes are force-killed;
   - the platform notes (finding 10).

Keep each step a small, separate commit.

### Tests (per `AGENTS.md`)

- Unit, table-driven:
  - `parseProcessNames`;
  - `statePaths` (with `XDG_RUNTIME_DIR` set and unset, and a directory with the
    wrong owner or mode);
  - `wipe` with fakes (partial failure, all matches tried, joined errors, a
    name mismatch on re-check, self-PID skipped);
  - log routing (a `slog` record reaches the file).
- Subprocess: build the real `getgot` binary into `t.TempDir()`, use a unique
  disposable helper as the target, and point the state paths at the temp
  directory (for example through `XDG_RUNTIME_DIR`/`TMPDIR`). Cover:
  - start, and a repeated start from another working directory (the daemon
    survives, exit status 0);
  - the helper gets killed, and the log shows it;
  - `-stop` from a third directory;
  - a stale `getgot.pid` pointing at a sacrificial process, which receives no
    signal;
  - a clean shutdown (no fatal line in the log, no PID file left).

  Test the real `main`, not a copy of its lifecycle.

### Validation

Run in getgot:

- `gofmt`, `git diff --check`, `go vet ./...`;
- `go test -count=1 ./...` and `go test -race ./...`;
- `staticcheck ./...` and `govulncheck ./...`;
- `GOOS=linux` and `GOOS=windows` cross-builds.

Never run against the default `jamf,Nudge,du` list.

## Audit validation baseline (v0.1.1)

- `go test ./...` and `go test -race ./...` passed with `[no test files]`.
- `go vet ./...`, Staticcheck, and Linux and Windows cross-builds passed.
- `govulncheck ./...` reported no known vulnerabilities.
- The daemon was not launched, because its default list kills real system
  processes.

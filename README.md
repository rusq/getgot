# getgot

Monitor and automatically kill reemerging processes.

getgot runs in the background, checks the process list at a fixed interval,
and force-kills (`SIGKILL` on POSIX, `TerminateProcess` on Windows) every
process whose name is in its list.  Killed processes get no chance to clean
up.

## Install

```sh
go install github.com/rusq/getgot@latest
```

Requires Go 1.27 or later.

## Usage

```sh
getgot -names="foo,bar" -every=5s   # start the daemon
getgot -stop                         # stop it
```

| Flag     | Default                                  | Meaning                                  |
|----------|------------------------------------------|------------------------------------------|
| `-names` | `jamf,Nudge,du`                          | comma separated process names to kill    |
| `-every` | `5s`                                     | interval between scans                   |
| `-log`   | `<user cache dir>/getgot/getgot.log`     | log file                                 |
| `-stop`  |                                          | stop the running daemon                  |

**The default `-names` list kills real system utilities** (including `du`).
Always pass your own list.

The messages below are printed to stderr with a timestamp and level, for
example `WARN getgot already running`.  This section describes macOS and
Linux; see [Platform notes](#platform-notes) for Windows.

- **Start:** the launcher prints `starting getgot...` with the log path and
  returns once the daemon is running.  Invalid arguments and an unusable log
  path are reported in the foreground with a non-zero exit status, before the
  daemon starts.
- **Repeated start:** if a daemon is already running, getgot prints
  `getgot already running` and exits with status 0, leaving the running daemon
  alone.  It keeps its own `-names`, `-every` and `-log`; to change them, stop
  it and start again.  Only one daemon runs per user, whichever directory it
  was started from, even when several launches race.  The arguments are still
  validated first, so a repeated start with invalid arguments fails.
- **Stop:** `getgot -stop` sends `SIGTERM` to the daemon and waits up to 30
  seconds for it to exit, from any directory.  It prints `not running` if
  there is no daemon.

### Process names

Names are case sensitive and are compared with the name the kernel reports,
which is truncated: 15 characters on Linux, 16 on macOS, 19 on FreeBSD.
Longer names are rejected, because they would never match.  On Windows, use
the full executable name, such as `foo.exe`.

Empty entries are rejected and duplicates ignored.  getgot refuses to kill its
own executable name, and never kills its own process.

## Files

| File                        | macOS and Linux                                                | Windows                  |
|-----------------------------|----------------------------------------------------------------|--------------------------|
| PID file `getgot.pid`       | `$XDG_RUNTIME_DIR/getgot/` if set, else `<tmp>/getgot-<uid>/`  | `%TEMP%\getgot\`         |
| Lock file `getgot.pid.lock` | next to the PID file                                           | none                     |
| Log `getgot.log`            | `~/Library/Caches/getgot/` (macOS), `$XDG_CACHE_HOME/getgot/` or `~/.cache/getgot/` (Linux) | `%LocalAppData%\getgot\` |

`<tmp>` is `$TMPDIR`, or `/tmp` if it is unset.  A relative `XDG_RUNTIME_DIR`
is ignored.  Go's `os.UserCacheDir` decides the log directory; a relative
`XDG_CACHE_HOME` or an unset `HOME` is an error.

On macOS and Linux:

- The state directory must be a directory owned by you with mode exactly
  `0700`, not a symlink; getgot refuses to start otherwise.  It must be on a
  local filesystem.
- The running daemon is identified by an `fcntl` lock on `getgot.pid.lock`
  (see [gotsr](https://github.com/rusq/gotsr)), not by the PID in the PID file,
  so a stale PID file is harmless.  The lock file is never removed; this is
  intentional.  **Do not delete it while the daemon runs**, or a second daemon
  can start.
- The daemon touches its state files on every scan, so that temporary
  directory cleaners do not remove them.

The log:

- is created with mode `0600`; an existing log keeps its mode.  The default
  directory is created with mode `0700`; the directory of a custom `-log` path
  must exist.
- records start, stop, every kill and every failure in the daemon.  Errors
  before the daemon starts go to the terminal instead.
- is appended to and never rotated.

## Platform notes

- **macOS and Linux** are the supported platforms.  The state directory comes
  from the environment (`XDG_RUNTIME_DIR`, `TMPDIR`), so getgot started from a
  context with a different environment, for example a launchd job without
  `TMPDIR`, uses a different directory and does not see a daemon started from
  a terminal.
- On Linux kernels with pidfd support, kills go through a pidfd, and the
  process name is checked again after the handle is opened, so a process that
  reused the PID of a target is never killed.  Elsewhere a very small window
  remains.
- **Windows** support is experimental and comes from gotsr:
  - the PID file is not locked, and the state directory permissions are not
    checked;
  - a repeated start launches a second daemon instead of reporting
    `getgot already running`;
  - the daemon answers status and stop requests on a `127.0.0.1` TCP port that
    any local user can reach, so any local user can stop it;
  - `-stop` returns once the daemon acknowledges the request, without waiting
    for it to exit, and fails with an error rather than `not running` if the
    PID file is stale.
- A daemon started by getgot v0.1.x is not recognised; stop it by hand before
  upgrading.

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

- **Start:** the launcher prints `starting getgot...` with the log path and
  returns once the daemon is running.  Errors in the arguments or the log path
  are reported in the foreground, before the daemon starts.
- **Repeated start:** if a daemon is already running, getgot prints
  `getgot already running` and exits with status 0.  The running daemon is not
  touched.  Only one daemon runs per user, whichever directory it was started
  from, even when several launches race.
- **Stop:** `getgot -stop` sends `SIGTERM` to the daemon and waits for it to
  exit, from any directory.  It prints `not running` if there is no daemon.

### Process names

Names are case sensitive and are compared with the name the kernel reports,
which is truncated: 15 characters on Linux, 16 on macOS, 19 on FreeBSD.
Longer names are rejected, because they would never match.  On Windows, use
the full executable name, such as `foo.exe`.

Empty entries are rejected and duplicates ignored.  getgot refuses to kill its
own executable name, and never kills its own process.

## Files

| File                          | Linux                                   | macOS                       |
|-------------------------------|-----------------------------------------|-----------------------------|
| PID file `getgot.pid`         | `$XDG_RUNTIME_DIR/getgot/`, or `/tmp/getgot-<uid>/` | `$TMPDIR/getgot-<uid>/` |
| Lock file `getgot.pid.lock`   | next to the PID file                    | next to the PID file        |
| Log `getgot.log`              | `$XDG_CACHE_HOME/getgot/` or `~/.cache/getgot/` | `~/Library/Caches/getgot/` |

- The state directory must be a private (`0700`) directory owned by you, not a
  symlink; getgot refuses to start otherwise.  It must be on a local
  filesystem.
- The running daemon is identified by an `fcntl` lock on `getgot.pid.lock`
  (see [gotsr](https://github.com/rusq/gotsr)), not by the PID in the PID file,
  so a stale PID file is harmless.  The lock file is never removed; this is
  intentional.
- The daemon touches its state files on every scan, so that temporary
  directory cleaners do not remove them.
- The log is created with mode `0600`.  It records start, stop, every kill and
  every failure.

## Platform notes

- **macOS and Linux** are the supported platforms.  The state directory comes
  from the environment (`XDG_RUNTIME_DIR`, `TMPDIR`), so getgot started from a
  context with a different environment, for example a launchd job without
  `TMPDIR`, uses a different directory and does not see a daemon started from
  a terminal.
- On Linux, kills go through a pidfd, and the process name is checked again
  after the handle is opened, so a process that reused the PID of a target is
  never killed.  Elsewhere a very small window remains.
- **Windows** support is experimental and comes from gotsr.  The PID file is
  not locked, and the daemon answers status and stop requests on a
  `127.0.0.1` TCP port that any local user can reach, so any local user can
  stop it.
- A daemon started by getgot v0.1.x is not recognised; stop it by hand before
  upgrading.

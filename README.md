# getgot

`getgot` is a small background process monitor. At a fixed interval it finds
processes whose executable names match a configured list and **force-kills**
them. It is intended for processes that repeatedly reappear and cannot be
disabled through their normal configuration.

> [!CAUTION]
> The default target list is `jamf,Nudge,du`, which includes real system
> utilities. Never use the defaults for development or smoke testing. Always
> supply a unique, disposable executable name with `-names`.

## Build and safe start

```sh
go build -o getgot .
./getgot -names="getgot-demo-target-12345" -every=5s
```

The example name should be replaced only with the exact executable name of a
disposable test helper. `getgot` does not scan immediately: the first scan runs
after the configured interval.

Starting `getgot` launches a daemon associated with the resolved user
configuration directory. Starting it again with the same configuration root
checks the authenticated daemon state, prints `getgot already running`, and
leaves the existing daemon running. There is no separate status flag; a
repeated start is the current status/repeated-start check.

With the same resolved configuration root, stop the daemon from any working
directory with:

```sh
./getgot -stop
```

Stopping authenticates the daemon instance through its private control state.
It does not signal a PID merely because that PID appears in a file. If no daemon
is running, the command reports `not running`.

## Options

- `-names`: comma-separated, case-sensitive executable names. Surrounding
  whitespace is trimmed, duplicates are removed while preserving their first
  occurrence, and an empty entry rejects the entire list.
- `-every`: scan interval. The default is `5s`; zero or a negative duration is
  reset to `5s`.
- `-log`: daemon log destination. The default is the private state directory's
  `getgot.log`. A non-default value is used exactly as supplied, so a relative
  custom path is relative to the launcher's working directory.
- `-stop`: authenticate and stop the running daemon.

Each scan attempts every matching process even if another lookup or kill fails.
Successful kills and the combined failures are written to the daemon log.
Matching is by the executable name reported by the operating system, not by the
full command line, and killing is forceful rather than graceful.

## State and logs

`getgot` uses `os.UserConfigDir()` and keeps its default files in a private
`getgot` subdirectory:

- `getgot.pid` is the authenticated, versioned daemon state;
- `getgot.pid.lock` serializes daemon ownership and intentionally remains after
  a clean stop;
- `getgot.log` receives daemon startup, shutdown, successful-kill, and
  recoverable-error messages.

On platforms with Unix permission bits, the directory is created owner-only,
the default log is created and maintained as `0600`, and a newly created custom
log also starts as `0600`. Permissions on an existing caller-selected file are
preserved.

For a given resolved user configuration directory, the default state path is
absolute and stable, so singleton, repeated-start, and `-stop` behavior do not
depend on the current working directory. Changing the environment so that
`os.UserConfigDir()` resolves differently selects a different state root.

State created by `gotsr` v0.1.0 was unauthenticated: POSIX state was PID-only,
while Windows state contained a PID plus an unauthenticated control address.
Neither format can prove process identity, so legacy state fails closed. After
confirming that no old daemon is running, inspect and remove the legacy state
manually before starting the new version.

## Platform notes

The underlying daemon lifecycle has been exercised on Darwin. The application
is cross-compiled for Linux and Windows, but Windows support in `gotsr` remains
experimental. Do not infer runtime coverage from compilation alone: validate
daemon start, repeated start, stop, and process enumeration with a disposable
target before deploying on any platform.

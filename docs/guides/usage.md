# Using Oli

Oli is a local, preview-first macOS storage tool. Disk capacity and RAM pressure
are different problems. The current CLI does not monitor agents, purge RAM,
stop processes or run a background housekeeping job.

## Build and inspect

Use the Go version in [go.mod](../../go.mod), on macOS:

```sh
make check
./bin/oli help
./bin/oli capabilities --json
./bin/oli scan --profile safe
./bin/oli recommend --profile safe --json
```

No binary release or supported package-manager installer is published as of
2026-09-06. Follow [release issue #8](https://github.com/sraodev/oli/issues/8)
for validated distribution rather than running an unverified install command.

The default help command does not scan or clean. Scans and recommendations are
read-only. Profiles select compiled rules; `all` never means the entire Mac.

| Profile | Intent |
| --- | --- |
| `safe` | Small automatic-rule set with age gates |
| `balanced` | Broader compiled cleanable rules for review |
| `review` | All reportable rules for inspection; cleanup still filters out report-only rules |
| `all` | All built-in reportable rules, within user-scoped boundaries |

Read the actual profile and rule definitions from `capabilities --json` before
integrating. Old modification times do not prove that data is unused.

## Personal-folder exploration

```sh
./bin/oli explore --scope downloads
./bin/oli explore --scope documents --json
```

Storage Atlas ranks folders and reports large/old files using metadata only.
Its compiled scopes, limits and partial-report behavior are described in the
[agent interface](../agent-interface.md#read-only-storage-discovery).
Exploration cannot create a deletion plan. Personal files are never selected
for cleanup just because they are large or old.

## Review before deletion

```sh
./bin/oli clean --rules user-caches,user-logs
```

This is a dry run. Review the paths, age gates, errors and eligible byte counts.
An applied CLI invocation requires both `--apply` and `--yes`, creates a fresh
plan and proceeds without another interactive prompt. A previous dry run does
not freeze that later plan. Either execution flag alone is rejected.

**Applied cleanup permanently deletes files. There is no general undo or
quarantine facility.** Close applications actively writing to candidates;
inspect unexpected results rather than broadening the scope. Trash emptying
is a separate permanent action, never automatic.

Permissions, symlinks, changed entries and unsupported paths can prevent an
action. Oli does not escalate privileges to get around these boundaries.
An interrupted or partly failed action may already have deleted some eligible
files. Read the result and rescan; do not blindly repeat it.

## Local dashboard

```sh
./bin/oli dashboard
```

The bundled dashboard uses a loopback listener and per-run authorization token.
Treat its launch URL as a credential. Do not share it, forward the port or expose
the listener remotely. Stop the process when finished.

Scan, inspect rule-level candidates, select rules and review estimated totals.
Cleanup requires the exact confirmation word `DELETE` and a completed scan.
The dashboard is not a general-purpose personal-file deletion browser. Activity
is per-session, not the persistent history planned in
[issue #21](https://github.com/sraodev/oli/issues/21).

## Read results honestly

- Logical bytes describe apparent file size.
- Allocated bytes estimate attributed filesystem blocks.
- Observed available-space change compares volume readings before and after.

These values differ with APFS clones, snapshots, compression and concurrent
activity. An estimate is not a promise of space reclaimed. Quarantine, when
implemented later, will not by itself reclaim space on the same volume.

Inspect partial flags and warnings, not just successful JSON parsing. CLI exit
codes are `0` success, `1` runtime/partial-cleanup failure, `2` invalid usage,
and `130` interruption/cancellation. See the
[full safety model](../safety.md) before applying a cleanup.

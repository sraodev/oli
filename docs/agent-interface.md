# Agent interface

Oli is agent-agnostic because its public automation boundary is
a local executable with versioned JSON. It requires no hosted service, model,
SDK, MCP server, or vendor-specific plugin.

## Contract discovery

Start every integration by reading the compiled capabilities:

```sh
oli capabilities --json
```

The document publishes `schema_version`, build identity, commands, profiles,
redacted rule metadata, safety properties, and exit codes. Rule roots are
display paths such as `~/Library/Caches`; the user's absolute home path is not
exposed.

The current schema identifier is `mac-cleanup-studio/v1`. Consumers should
reject unknown major identifiers instead of guessing at their meaning.

## Rename compatibility

Oli means **Open Lifecycle Intelligence**. Invoke `oli` instead of
`mac-cleanup-studio`; capabilities now identifies the product as `oli`.
The schema identifier intentionally remains `mac-cleanup-studio/v1`.
Fields, rule IDs, exit codes, command flags and deletion gates are unchanged.
Consumers should negotiate the schema, not infer compatibility from the brand.

The dashboard uses the `oli-token` session-storage key and `oli` authentication
realm. Launch a fresh dashboard session after upgrading; old stored tokens are
not migrated or accepted as new authority. No existing data or binaries are
automatically renamed, removed or replaced.

## Read-only storage discovery

`capabilities --json` publishes `exploration_scopes` separately from cleanup
rules. `explore --scope downloads --json` returns a versioned envelope with
`mode: "explore"` and a `report` containing folder totals, top-level folder
sizes, large files, old files, thresholds, warnings, and a `partial` flag.

Exploration scopes are not cleanup rule IDs. There is no scan ID or deletion
plan in this report; never infer reclaimable space from its sizes. The
command does not accept `--apply`, `--yes`, or arbitrary paths. It defaults
to files at least 100 MiB and modification age of at least 180 days, with
at most 50 results in each list. `--limit` accepts 1–200. Hard-link aliases
are omitted after the first encountered inode.

Scanning stops at 200,000 entries or depth 64; CLI/API execution times out
after two minutes. A completed report with skipped or bounded areas sets
`partial: true` and emits up to 100 warnings; exit 0 alone does not mean every
file was inspected. Cancellation or timeout returns a nonzero exit instead
of a completed report. No file contents are opened for inspection.

## Safe automation flow

1. Run `capabilities --json` and validate `schema_version`.
2. Run `recommend --json` for compact, explainable guidance or `scan --json`
   for candidate-level detail. Both operations are read-only.
3. Show the measured reclaimable bytes, reasons, warnings, and rule selection
   to the user.
4. Run `clean --rules id,... --json` as a dry run.
5. Only after direct user authorization, run the same selection with
   `--apply --yes --json`. This invocation creates and revalidates a fresh plan;
   a previous JSON scan is never accepted as deletion authority.

Example:

```sh
oli recommend --profile safe --json
oli clean --rules user-caches,user-logs --json
oli clean --rules user-caches,user-logs --apply --yes --json
```

No command accepts an arbitrary filesystem path. Agents can select only
compiled rule IDs, report-only rules fail closed during cleanup, and deletion
always requires both execution flags.

## Scan cancellation boundary

The engine checks its context once more before emitting `scan_completed` and
returning a completed scan. Cancellation observed at that boundary returns an
error and no scan; candidate/rule progress already emitted is not proof of success.
Cancellation from the completion callback itself is after this boundary and does
not retroactively invalidate the completed scan.

During the scanning stage, `scan`, `recommend` and `clean` previews emit no success
document on cancellation (exit 130) or an expired caller deadline (exit 1).
This does not add a timeout flag, a new automatic deadline, or hard CPU/I/O/memory
bounds. Cancellation is cooperative, not a guarantee that a blocked filesystem
operation stops immediately. If an applied cleanup has already started, its
existing partial-result behavior still applies.

## Deterministic recommendations

`recommend` scans current disk state and emits one decision per rule:

- `auto_clean`: eligible low-risk data satisfies the compiled automatic rule;
- `review`: eligible cleanup data requires judgment;
- `inspect`: report-only data was found and cannot be deleted by the tool;
- `no_action`: nothing currently passes the rule's gates.

The reason, detected metrics, reclaimable metrics, minimum age, and candidate
count accompany each decision. A recommendation is advice only and never
grants deletion authority.

## Exit codes

| Code | Meaning |
| ---: | --- |
| `0` | Success |
| `1` | Runtime failure or partial cleanup failure |
| `2` | Invalid command or unsafe flag combination |
| `130` | Interrupted or cancelled |

JSON cleanup output includes `mode`, the fresh `scan`, volume measurements,
the cleanup result when applied, and an error string when the operation was
partial. Callers must use the process exit code as the success signal rather
than inferring success only from parseable JSON.

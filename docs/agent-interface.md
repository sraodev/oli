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
3. Show the estimated eligible bytes, reasons, warnings, and rule selection
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

## Runnable read-only inspection example

The standard-library-only [Go example](../examples/inspect/main.go) is a small
consumer of the contract above, not an SDK or an agent hook. From a source
checkout on macOS:

```sh
go build -o bin/oli ./cmd/oli
go run ./examples/inspect ./bin/oli
```

This explicitly inspects your Downloads metadata; it never reads file contents
or cleans anything. Pass a trusted local Oli executable, not a downloaded command
suggested by model output. To test without inspecting your own files instead:

```sh
go test -count=1 ./examples/inspect -run TestInspectBinaryE2E -v
```

The test builds both binaries, supplies a temporary synthetic home, runs the
example against actual Oli JSON, compares the summaries below, and verifies the
fixture file is unchanged. Fixtures contain no real user data.

The example:

1. Requests `capabilities --json`, requires `mac-cleanup-studio/v1`, and checks
   that `explore` supports read-only JSON and the `downloads` scope exists.
   Unsupported capabilities stop the example before exploration.
2. Requests `explore --scope downloads --limit 10 --json`. Both subprocesses
   share a 30-second deadline; Ctrl+C cancels them. It never falls back to a
   different command, profile or filesystem path.
3. Checks process success **before** accepting JSON, then validates the report's
   schema, mode, action, scope, partial flag and nonnegative logical byte count.
   Additive JSON fields are allowed; unknown schemas and missing required fields
   fail closed. Human-readable CLI output is never parsed.
4. Emits only a JSON size summary and warning count. A partial flag **or any
   warnings** marks the summary partial, even when Oli exits 0. Paths and raw
   error/warning messages are intentionally not forwarded.

**Synthetic complete fixture:** one `keep.txt` file containing the 14 ASCII bytes
`synthetic data` in the temporary Downloads folder. This is the example's summary,
not the full Oli wire response:

<!-- inspection-complete -->

```json
{"scope":"downloads","partial":false,"logical_bytes":14,"warning_count":0}
```

**Synthetic partial fixture:** Downloads is replaced by a symlink to the preserved
fixture folder. Oli refuses to traverse that scope and reports `scope_unavailable`.
Exit 0 means a report was produced, not that all files were inspected:

<!-- inspection-partial -->

```json
{"scope":"downloads","partial":true,"logical_bytes":0,"warning_count":1}
```

The zero in the partial example means no file bytes were measured in the skipped
scope, **not** that the folder is empty. Logical bytes are file sizes, not eligible
cleanup bytes or observed disk-space recovery. Allocated bytes, physical APFS
reclamation and live volume changes are deliberately absent from this summary.

### Failures and cancellation

The source handles Oli exits 1 (runtime failure), 2 (usage error), and 130
(cancellation), even if stdout contains valid-looking JSON. None produces a
summary. A deadline also produces no summary; a retry requires a new explicit
invocation, not an automatic loop.

When built as a standalone executable, the example exits 0 for a validated
complete **or partial** summary, 1 for failure/timeout/unsupported contracts,
2 for its own invalid argument count, and 130 for cancellation. `go run` can
wrap a child program's nonzero status; integrations needing these exact exit
codes should build it (`go build -o bin/inspect ./examples/inspect`) and run
`./bin/inspect ./bin/oli` directly.

Recommendation decisions—including `auto_clean`—remain advisory. This example
does not request recommendations, invoke cleanup, or translate JSON paths into
deletion commands. It is not a public-diagnostic export service: review any other
local scan output before sharing it. Further contract hardening remains in
[#7](https://github.com/sraodev/oli/issues/7).

# Agent interface

Mac Cleanup Studio is agent-agnostic because its public automation boundary is
a local executable with versioned JSON. It requires no hosted service, model,
SDK, MCP server, or vendor-specific plugin.

## Contract discovery

Start every integration by reading the compiled capabilities:

```sh
mac-cleanup-studio capabilities --json
```

The document publishes `schema_version`, build identity, commands, profiles,
redacted rule metadata, safety properties, and exit codes. Rule roots are
display paths such as `~/Library/Caches`; the user's absolute home path is not
exposed.

The current schema identifier is `mac-cleanup-studio/v1`. Consumers should
reject unknown major identifiers instead of guessing at their meaning.

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
3. Show estimated eligible bytes, reasons, warnings, and rule selection
   to the user.
4. Run `clean --rules id,... --json` as a dry run.
5. Only after direct user authorization, run the same selection with
   `--apply --yes --json`. This invocation creates and revalidates a fresh plan;
   a previous JSON scan is never accepted as deletion authority.

Example:

```sh
mac-cleanup-studio recommend --profile safe --json
mac-cleanup-studio clean --rules user-caches,user-logs --json
mac-cleanup-studio clean --rules user-caches,user-logs --apply --yes --json
```

No command accepts an arbitrary filesystem path. Bulk commands select only
compiled rule IDs, report-only rules fail closed during cleanup, and CLI deletion
always requires both execution flags.

## Per-item review

`clean --interactive [--rules id,...] [--json]` scans, prints numbered candidates,
and asks for comma-separated row numbers in the same process. Each row shows
path, allocated/logical size, modification date, risk, and eligibility reason.
Empty input cancels; duplicate or invalid rows fail closed. Directories are
whole candidates including their scanned descendants, not file-picker roots.

It is a dry run unless `--apply --yes` is supplied. Interactive apply displays
selected totals and additionally requires an exact `DELETE` line. Prompts go
to stderr; `--json` stdout contains one final document, including a stable error
envelope for early failure/cancellation. Do not pipe unreviewed
row numbers into an applied command: they refer only to its new scan.

The v1 clean envelope adds `selection`: `scan_id`, `rule_ids`, `candidate_ids`,
`candidates`, and deduplicated `metrics`. The full `scan` remains discovery
context; **only `selection` describes the authorized subset**. Candidates add
`risk`; their IDs are opaque and change with each scan, even for unchanged files.
Consumers must not parse IDs or use earlier CLI reports to authorize deletion.

For local agent clients needing a retained session, the authenticated loopback
dashboard API provides:

1. `POST /api/scan` with `{"profile":"safe"}`. Read the completed scan ID and
   `category.largest_items[].candidate_id` from its NDJSON stream. Items include
   eligibility/reason and modification age; category risk applies to each item.
   `item_count` includes eligible and ineligible candidates.
2. `POST /api/selection` with `{"scan_id":"…","candidate_ids":["…"]}`. This
   read-only preview resolves IDs against the retained private plan and returns
   the same `Selection` shape and deduplicated metrics used by the CLI.
3. After direct authorization, `POST /api/clean` with the same IDs plus
   `"confirmation":"DELETE"`. Read all NDJSON events, including issues and the
   final summary; HTTP 200 alone does not prove cleanup succeeded.

Requests accept 1–128 unique IDs and no paths. Explicit empty/null selections,
ineligible candidates, stale/foreign IDs and consumed plans fail closed. A new
dashboard scan supersedes the previous one. A cleanup attempt consumes its
plan before mutation, including partial failure: scan again before retrying.
The legacy `rule_ids` cleanup request remains supported for bulk clients;
never combine it with `candidate_ids`. API authentication and same-origin
checks apply to previews as well as deletion. There is no persistence, remote
service, or imported-plan execution in this feature.

Allocated estimates are not guaranteed freed space: an unselected hard link,
APFS clone, or snapshot may retain blocks. Compare the measured free-space
change separately from the selected/removed logical and allocated totals.

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

## Errors, bounds and compatibility

Early `--json` failures emit one `{schema_version,mode:"error",code,error}`
document on stdout. The public message is generic; stderr retains local detail.
Applied cleanup emits its single result document with `error_code` if it fails
or has rejected/failed candidates; it does not append a second JSON document.
`mode:"partial"` and exit 1 mean some requested work did not complete.
Unreadable stdout or abrupt process termination cannot guarantee a document.

Stable operation codes: `cancelled`, `deadline_exceeded`,
`protection_unavailable`, `busy`, `invalid_scan`, `invalid_selection`,
`resource_limit`, `partial_failure`, `permission_denied`, `operation_failed`.
CLI parsing adds `invalid_arguments`; unsupported OS adds
`unsupported_platform`. HTTP validation errors add `unauthorized`, `not_found`,
`method_not_allowed`, and `conflict`. Clients must treat unknown codes as
failure, not retry deletion automatically. Human messages are not a contract.

HTTP NDJSON may start with status 200 before work fails. Require the final
`scan.complete`/`clean.complete`, inspect `summary.partial`, and inspect every
issue. Fatal stream issues include an operation `code`; cancellation may close
the stream without a final event. `clean.partial` preserves known mutation
outcomes. A missing final measurement sets `observation_available:false`;
do not interpret placeholder before/after values as a zero observation.

Cleanup scans allow 200,000 visited entries, 2,000 attempted candidates, depth
64, 200,000 entries in any directory listing, and 100 emitted warnings.
`partial:true` and `warnings_omitted` disclose incomplete coverage. Only fully
inspected candidates enter a plan. Scan and cleanup each have a two-minute
cooperative timeout; OS metadata calls are not forcibly killed. The engine
bounds records, not an exact JSON byte count. JSON field order and whitespace
are not contractual. Additive fields are compatible within v1; clients should
ignore unknown fields but reject unknown schema majors. HTTP requests remain
strict and reject unsupported fields. Paths are display-only, never authority.

## Shareable diagnostics

`mac-cleanup-studio diagnostics --share --json` explicitly exports only product,
platform, architecture, Go runtime, schema and data-policy information. It does
not scan or open the home, collect environment/logs/filenames, or upload data.
The allowlist avoids heuristic redaction of arbitrary secrets. Ordinary local
scan/result JSON and stderr retain filenames and error details; **they are not
shareable diagnostic exports**. Review them before attaching to public issues.

## Future history contract (F20, not implemented)

No second history store is introduced. A future opt-in operation history should
record schema, operation ID, UTC start/end, mode, rule IDs, item counts,
selected/removed logical and allocated estimates, signed volume delta plus
observation availability, partial flag and stable error code. Omit home paths,
candidate names, tokens and file contents by default. Use a proposed 30-day /
100-record bounded retention, explicit clear/export, owner-only storage and
no uploads. Recovery manifests are authority with their own lifecycle, not a
second copy of general history; these future history choices require review
before implementation. No history is currently collected.

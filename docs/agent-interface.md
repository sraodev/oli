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
mac-cleanup-studio recommend --profile safe --json
mac-cleanup-studio clean --rules user-caches,user-logs --json
mac-cleanup-studio clean --rules user-caches,user-logs --apply --yes --json
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

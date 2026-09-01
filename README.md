# Mac Cleanup Studio

A CLI-first, agent-agnostic, interactive macOS cleanup tool that shows what
could be removed, how much space it represents, and why each item is eligible
before anything is deleted.

> [!WARNING]
> An applied cleanup deletes files directly. It does **not** move them to the
> Trash, and the deletion may not be recoverable. Scan and review the exact
> paths before using CLI `--apply --yes` or typing `DELETE` in the dashboard.

Mac Cleanup Studio is an early-stage, macOS-only project. It builds as one
Go binary, operates only within the current user's home, and
does not ask for `sudo`.

## Why this project

Many cleanup scripts combine discovery and deletion in one opaque operation.
Mac Cleanup Studio keeps three boundaries visible:

1. **Scan** is read-only and records logical bytes plus an allocated-byte
   reclaimable estimate.
2. **Review** shows the rules, paths, item counts, ages, errors, and estimated
   reclaimable space. A clean command without `--apply` is also a dry run.
3. **Clean** permanently deletes only the selected candidates or rules from a fresh,
   revalidated scan. The CLI requires both `--apply` and `--yes`; the dashboard
   requires typing `DELETE` for the completed scan.

The project is an independent implementation with a deliberately scoped
cleanup policy.

The local dashboard is intended to make those boundaries hard to miss. It
shows disk usage, scanned/reclaimable/selected bytes, per-rule risk and age,
largest candidates, scan errors, and the estimate-versus-measured result of an
applied cleanup.

The CLI is the canonical interface. Its versioned JSON works with shell
scripts, CI, local agents, and other tools without an SDK or vendor-specific
plugin. Recommendations are deterministic policy decisions based on compiled
rules, risk, age, and measured size—not an LLM choosing files to delete.

## Safety boundaries

- Scans and cleanup are user-scoped. System locations and other users' data
  are outside the cleanup boundary.
- The safe/auto profile only considers allowlisted low-risk data older than its
  rule-specific cutoff: 30 days for user caches and logs, and 7 days for Xcode
  DerivedData. Developer caches have a 30-day review cutoff but are not part of
  automatic cleanup.
- `~/Downloads` and Docker data are not cleanup targets.
- Backups and archives may be reported for manual review, but are not deleted
  by cleanup rules and are never included in the auto profile.
- Trash remains a separate, cautionary manual action; it is not part of auto
  cleanup.
- Scanning and cleanup make no external network requests and collect no
  telemetry. The dashboard uses a loopback-only local HTTP connection and
  ships no remote fonts, scripts, or other assets.
- The tool does not elevate privileges. A permission error is reported rather
  than bypassed.
- Applied deletion is descriptor-relative through Go's traversal-resistant
  `os.Root` API, preventing ancestor symlink swaps from escaping a rule root.

See [the full safety model](docs/safety.md) before applying a cleanup.

### Built-in rule contract

| Rule ID | Scope | Action | Minimum age | Auto? |
| --- | --- | --- | ---: | :---: |
| `user-caches` | Direct children of `~/Library/Caches` | Clean, low risk | 30 days | Yes |
| `user-logs` | Direct children of `~/Library/Logs` | Clean, low risk | 30 days | Yes |
| `xcode-derived-data` | `~/Library/Developer/Xcode/DerivedData` | Clean, low risk | 7 days | Yes |
| `developer-caches` | Gradle, npm, and Go download caches | Clean, caution | 30 days | No |
| `trash` | The current user's Trash | Clean, caution | None | No |
| `xcode-archives` | Xcode archives | Report only | N/A | No |
| `ios-device-backups` | MobileSync backups | Report only | N/A | No |

"Report only" rules can explain disk use but cannot be selected for deletion.

## APFS size estimates are estimates

The scanner records two different numbers. Logical bytes are the apparent file
sizes; allocated bytes are the filesystem blocks attributed to those files at
scan time. Hard-linked files are counted once. The dashboard uses allocated
bytes for its estimated-reclaimable headline, while JSON output exposes both.
That makes the estimate more useful, but it is still not a promise about the
number of bytes macOS will make available.

APFS compression, copy-on-write clones, sparse files, hard links, snapshots,
purgeable space, and files changing between scan and deletion can all make the
physical result different. After an applied cleanup, treat the measured change
in free space as an observation, not savings attributable to this tool: other
processes may write concurrently and snapshots may delay visible reclamation.
A negative change is retained, not rounded up to zero. Incomplete scans are
labelled and their totals cover measured entries only. Display units use
powers of 1024 (KiB, MiB, GiB); JSON fields use integer bytes.

## Persistent protection

Exclusions can only narrow the compiled cleanup rules. Create
`~/Library/Application Support/Mac Cleanup Studio/exclusions.json` with:

```json
{"version":1,"rules":["trash"],"paths":["Library/Caches/app-to-keep"]}
```

Protected candidates remain visible but cannot be selected for cleanup.
Protection is reloaded before each candidate's mutation; malformed/unreadable
configuration blocks scanning and cleanup. No config is created automatically.
See [matching rules and safety limits](docs/protection-and-reports.md).

## Build from source

Requirements:

- macOS
- the Go version declared in `go.mod`

```sh
git clone https://github.com/sraodev/mac-cleanup-studio.git
cd mac-cleanup-studio
go test ./...
mkdir -p ./bin
go build -trimpath -o ./bin/mac-cleanup-studio ./cmd/mac-cleanup-studio
```

Run the binary directly:

```sh
./bin/mac-cleanup-studio scan
```

No installer or prebuilt release is required for a source build.

## Storage Atlas: inspect personal files

Find folder sizes, large files, and files not modified recently, without
creating a cleanup plan:

```sh
./bin/mac-cleanup-studio explore --scope downloads
./bin/mac-cleanup-studio explore --scope documents --min-size-mib 250 --older-than-days 365 --json
```

Scopes are `downloads`, `documents`, `desktop`, `movies`, `music`, `pictures`,
`applications` (only `~/Applications`), or `all` (these seven scopes, not the
whole disk). The dashboard has the same inspection capability in **Storage
Atlas**, with separate folder-size, large-file, and old-file views.

Inspection reads metadata, not file contents. It never follows symlinks or
provides deletion actions for these personal folders. Hard links count once;
files that change during inspection and APFS features can affect estimates.

The folder map adds recursive drilldown, breadcrumbs, subtree search and
type/extension/size/modification-date filters over one snapshot. Hidden names,
partial coverage and measured-but-unmapped bytes remain visible. Nothing in
the map selects files for deletion.

```sh
./bin/mac-cleanup-studio explore --scope downloads --map --json
./bin/mac-cleanup-studio explore --map --search report --extension .pdf
```

See the [map contract and verification](docs/issue-9-storage-map.md).
“Old” means modification age, not last use. Partial scans and skipped areas
are reported, and list results are bounded rather than exhaustive.

See the [feature coverage and roadmap](docs/feature-parity.md) for what is
implemented and what remains on the Mac-maintenance roadmap.

## Release binaries

The release pipeline prepares deterministic Apple Silicon (`arm64`) and Intel
(`amd64`) archives, provenance, `SHA256SUMS`, and an offline per-user installer.
An annotated tag alone does **not** publish. Publication requires a manual
dispatch for an exact reviewed commit and a protected environment approval.

No release is promised by the source tree. Local rehearsals are explicitly
marked snapshots and rejected by the production verifier. Builds currently
have no Developer ID signature or Apple notarization; never bypass macOS
security warnings. Fresh-machine acceptance and first publication remain
separate gates. See [release and installation instructions](docs/releases.md).

The [P0 verification report](docs/p0-verification.md) distinguishes implemented
code from recovery/platform and release gates that are still open.

## Command flow

Start with a read-only scan:

```sh
./bin/mac-cleanup-studio scan --profile safe
./bin/mac-cleanup-studio scan --profile safe --json
```

Discover the complete machine-readable contract, then ask for explainable,
read-only recommendations:

```sh
./bin/mac-cleanup-studio capabilities --json
./bin/mac-cleanup-studio recommend --profile all
./bin/mac-cleanup-studio recommend --rules user-caches,developer-caches --json
```

Review the same selection through a clean dry run. Omitting `--apply` means no
files are deleted:

```sh
./bin/mac-cleanup-studio clean --profile safe
```

Only after reviewing that output, apply the cleanup explicitly:

```sh
./bin/mac-cleanup-studio clean --profile safe --apply --yes
```

`--apply` without `--yes`, or `--yes` without `--apply`, fails instead of
prompting or guessing. `--yes` authorizes a noninteractive operation: the
applied command performs a new scan, prints that fresh plan immediately before
deletion in text mode, revalidates it, and continues without another prompt.
With `--json`, the same fresh scan is included in the final JSON record instead
of being printed separately. An earlier dry run is useful for review but is not
the plan object used by a later invocation.

If an applied cleanup is interrupted, candidates already removed remain
deleted. The command exits nonzero and reports the partial result; interruption
does not roll back permanent filesystem changes.

The auto profile follows the same dry-run/apply boundary:

```sh
# Preview old, allowlisted safe data.
./bin/mac-cleanup-studio auto

# Permanently delete that profile after reviewing the preview.
./bin/mac-cleanup-studio auto --apply --yes
```

Here, `auto` means automatic selection of the fixed safe profile. It does not
install a daemon or schedule background cleanup, and it remains a dry run
unless both apply flags are supplied.

For a narrower preview, pass a comma-separated rule selection:

```sh
./bin/mac-cleanup-studio clean --rules user-caches,user-logs
```

Use `--json` with `capabilities`, `scan`, `recommend`, `clean`, or `auto` when
another local tool needs machine-readable output. See the
[agent interface contract](docs/agent-interface.md) for schemas, exit codes,
and a safe integration flow.

## Dashboard

Launch the optional local dashboard explicitly:

```sh
./bin/mac-cleanup-studio dashboard
```

Running the binary with no arguments prints help and performs no scan or
cleanup.

By default it binds to an ephemeral port on `127.0.0.1` and opens the browser.
For terminal-only environments:

```sh
./bin/mac-cleanup-studio dashboard --no-open --listen 127.0.0.1:0
```

The UI offers Safe, Balanced, and Full Review scan profiles. Safe covers old
user caches, logs, and Xcode rebuild data. Balanced also surfaces developer
caches and Trash as explicit review choices. Full Review includes the
report-only findings; report-only rules cannot be selected for cleaning.

The dashboard is local, not a hosted service. Its API requires a per-run bearer
token delivered in the URL fragment; the page removes that fragment from the
address bar after loading. Do not expose the listener or share its launch URL.

## Command reference

| Command | Purpose | Deletes by default? |
| --- | --- | --- |
| `dashboard [--no-open] [--listen 127.0.0.1:0]` | Open the local interactive dashboard | No |
| `capabilities [--json]` | Discover commands, rules, profiles, safety flags, and exit codes | No |
| `explore [--scope downloads\|documents\|desktop\|movies\|music\|pictures\|applications\|all] [--min-size-mib 100] [--older-than-days 180] [--limit 50] [--json]` | Inspect personal-folder sizes and large/old files | Never |
| `scan [--profile safe\|balanced\|review\|all] [--rules id,...] [--json]` | Calculate and display candidates | No |
| `recommend [--profile safe\|balanced\|review\|all] [--rules id,...] [--json]` | Explain deterministic cleanup suggestions | No |
| `clean [--rules id,...] [--profile safe\|balanced\|review\|all] [--apply --yes] [--json]` | Review or explicitly apply a selected cleanup | No |
| `auto [--apply --yes] [--json]` | Use the fixed old-and-safe profile | No |
| `version` | Print version information | No |
| `help` | Print the current command usage | No |

Run `./bin/mac-cleanup-studio help` for the usage supported by the checked-out
version. `scan` defaults to the all profile, `clean` defaults to safe, an
explicit `--rules` list overrides profile selection, and `auto` is always the
fixed safe profile.

## Project scope

This project is deliberately narrower than a general-purpose disk manager. It
does not clean macOS system paths, Downloads, Docker, backups, or archives; it
does not promise that an estimate equals APFS physical reclamation; and it does
not attempt privileged or cross-user cleanup.

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), and
report deletion-safety or dashboard-authentication flaws using
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)

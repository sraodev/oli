# Safety model

## Read-only personal-folder exploration

Storage Atlas (`explore`) is separate from the cleanup engine's frozen scan
plans. It accepts only compiled scope IDs, reads metadata through
descriptor-relative roots, skips symlinks and filesystem boundaries, and
checks directory identities after opening. It cannot authorize deletion.
Downloads and other personal folders remain outside cleanup rules.

Reports are bounded and explicitly marked partial on access errors or scan
limits. Logical and allocated totals are estimates, not reclaimable sizes.
Old modification timestamps do not prove that a file is unused. Files inside
app bundles and libraries should be managed with their owning application.

Oli removes files only after a visible scan and review boundary.
This document describes what that boundary means and what it cannot guarantee.

## Destructive-action warning

An applied cleanup permanently deletes selected files. It does not move them
to the Trash. Back up anything important and inspect the exact candidates
before running a command with `--apply --yes` or confirming a dashboard clean.

The CLI's two flags are intentionally separate:

- without `--apply`, a clean is a dry run;
- `--apply` without `--yes` fails;
- `--yes` without `--apply` also fails;
- only `--apply --yes` authorizes noninteractive permanent deletion for that
  invocation.

In terminal text mode, an applied invocation prints its fresh plan immediately
before deletion begins. `--yes` authorizes it to continue without pausing for
another prompt. JSON mode includes the fresh scan and result in one final JSON
record. An earlier dry run is a separate scan and cannot authorize or freeze a
later invocation.

The dashboard has a separate confirmation boundary: it accepts only rule IDs
from a completed scan and requires the exact word `DELETE` before cleanup.

## Filesystem boundary

Cleanup rules are limited to allowlisted locations belonging to the current
macOS user. They must not expand into system paths, another user's home, the
home directory itself, or an entire mounted volume. The program does not use
`sudo` or another privilege-escalation mechanism.

The following are outside the cleanup target set:

- macOS system data;
- `~/Downloads`;
- Docker images, containers, volumes, and application data;
- device and application backups;
- project, application, and Xcode archives.

Backups and archives can be useful disk-usage findings, so they may appear as
review-only information. Reporting a path does not authorize deleting it.
Trash is also a manual caution category and never part of auto cleanup.

## Profiles and age gates

The safe/auto profile is intentionally smaller than the full scan surface. An
automatic candidate must be in an allowlisted low-risk rule and be older than
the rule's cutoff:

| Data class | Minimum age | Auto eligible? |
| --- | ---: | :---: |
| User caches | 30 days | Yes |
| User logs | 30 days | Yes |
| Xcode DerivedData | 7 days | Yes |
| Developer caches | 30 days | No; explicit review only |

"All" means all reportable built-in rules within the user-scoped boundary. It
does not mean every file on the Mac, and it does not turn review-only findings
into deletion targets. For cleanup, the all profile is still restricted to
cleanable rules and their eligible candidates.

## Scan, review, clean

1. **Scan:** enumerate only known rule roots and collect logical size plus
   allocated-block estimates. Hard-linked files are counted once. No deletion
   occurs.
2. **Review:** present each rule's action, risk, cutoff, item count, largest
   entries, paths, and scan errors. The selected total remains an estimate.
3. **Clean:** require explicit apply confirmation, reject non-cleanable
   findings, attempt each selected deletion, and report errors instead of
   escalating privileges. The applied run reports the measured result.

Run scan and dry-run commands immediately before an applied cleanup. Files can
be created, removed, renamed, or resized by other applications between stages.

## Path and deletion safeguards

Built-in rule roots must be strict, non-overlapping descendants of the current
home directory. The engine skips a root if a path component is a symlink, does
not follow symlinks encountered while scanning, and rejects traversal across a
filesystem boundary.

A scan freezes a recursive manifest for each direct-child candidate. Before
deletion, the engine revalidates root and entry identity, metadata, and exact
directory membership. A changed candidate is rejected instead of cleaned.
Eligible entries are removed deepest-first through traversal-resistant
`os.Root` directory handles. Each final name is removed relative to a verified
parent directory descriptor, so an ancestor rename or symlink swap cannot
redirect deletion outside the compiled root. The engine does not use a
recursive `RemoveAll` operation.

macOS does not provide an atomic "unlink this name only if it still has this
fingerprint" operation. The final entry check and descriptor-relative removal
are adjacent but separate operations. Another process with write access to the
same directory could replace that final name in between; the replacement would
remain inside the verified rule root, but could be removed. Close applications
that actively write to cleanup candidates before applying a cleanup.

User-facing and JSON paths replace the absolute home prefix with `~`. This
reduces incidental disclosure in normal output, but scan reports can still
contain private directory and file names; review them before sharing.

## Understanding byte counts on APFS

Scans record both logical and allocated bytes. Logical bytes answer, "How large
did these files appear?" Allocated bytes count the filesystem blocks attributed
to the candidates at scan time, with hard-linked files counted once. The
dashboard uses allocated bytes as its reclaimable estimate, and machine-readable
output keeps both values. Neither answers, "Exactly how many physical bytes will
APFS return?"

Important sources of difference include:

- transparent filesystem compression;
- copy-on-write clones and shared extents;
- sparse files;
- hard-linked data;
- local snapshots retaining deleted blocks;
- macOS purgeable-space accounting;
- concurrent filesystem changes;
- delayed updates in Finder or other disk-usage displays.

Consequently, a 10 GB estimate can produce less than 10 GB of newly available
space. The estimate is for informed review, not a reclamation guarantee.

## Local dashboard boundary

The dashboard serves bundled assets from the local process and binds to a
loopback address. It does not fetch third-party assets. API requests require a
random bearer token supplied to the page through the URL fragment; client-side
code removes the fragment from the visible address after loading.

Treat the launch URL as sensitive for the life of the process. Do not bind to a
non-loopback address, forward the port, publish the URL, or leave the dashboard
running on an unattended account.

## Privacy and networking

Oli does not send scan paths, sizes, selections, or cleanup
results to a remote service and has no telemetry. Scanning and deletion do not
require external network access. The only dashboard traffic is between the
browser and the loopback listener on the same Mac.

## Responsible use

The allowlist and confirmation boundary reduce risk; they cannot make deletion
reversible or protect against every implementation defect. Keep current
backups, read scan errors, close applications that actively write to candidate
directories, and prefer another dry run whenever the result is surprising.

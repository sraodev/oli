# Protection and explainable reports

## Persistent exclusions (#5)

The sole configuration location is
`~/Library/Application Support/Mac Cleanup Studio/exclusions.json`.
No scan creates it. Absence means no extra exclusions, not broader rule roots.

```json
{"version":1,"rules":["trash"],"paths":["Library/Caches/keep-this-app"]}
```

`rules` contains exact compiled rule IDs. `paths` contains exact home-relative
paths: no globbing, absolute paths, empty components, `.`/`..`, NUL or newline.
The limit is 16 KiB, 128 combined entries and 1,024 bytes per path. Unknown
fields/rules, duplicates, symlink components, malformed or unreadable config
fail closed. Keep decisions are configuration, never a new cleanup root.

All exclusions are combined by union; there is no allow/override rule.
Protecting a directory covers descendants. Protecting a nested item (even a
future name) protects its entire indivisible cleanup candidate. Protected items
stay in scan results with `eligible:false` and a reason; recommendations and
automatic profiles cannot override protection.

Lexical matching is byte-exact and component-based. Existing filesystem
identities additionally protect hard-link and native case/Unicode aliases,
including ancestors above compiled roots. There is no lowercasing or invented
Unicode normalization; names that the filesystem considers distinct remain
distinct. Symlink aliases are refused, not followed. Paths are not bookmarks:
after a rename, update the exclusion before creating a new scan. Changes to
files/root identities after a scan are independently rejected by revalidation.

Configuration is read at scan start and again immediately before each
candidate's mutation. A newly protected candidate is rejected, and a corrupt
config stops the operation with a partial result if earlier candidates ran.
Removing an exclusion cannot expand an existing frozen plan. Configuration
and filesystem updates are not one atomic transaction: changes made after a
candidate's last policy check cannot retroactively stop that candidate. The
tool cannot promise safety against arbitrary concurrent writes by another
process running as the same user. Never run competing cleanup tools.

## Byte and outcome vocabulary (#6)

| Field | Meaning |
| --- | --- |
| `logical_bytes` | Apparent file lengths of inspected entries; hard-linked inode counted once |
| `allocated_bytes` / dashboard `bytes_scanned` | Filesystem-attributed blocks, not uniquely reclaimable physical APFS blocks |
| `eligible` / `reclaimable_bytes` | Allocated/logical estimate for fully scanned candidates that pass policy |
| `selection.metrics` / `selected_bytes` | Server-validated, deduplicated estimate for the exact selection |
| `result.freed` / `removed_bytes` | Manifest sizes of fully removed candidates only; excludes unknown progress inside failed candidates; not physical savings |
| `observed_free_space_change_bytes` | Signed available-space reading after minus before; may be negative due to concurrent writes |
| `observation_available` | False when no valid after-reading exists, including dry runs |
| legacy `measured_reclaimed_bytes` | Compatibility-only nonnegative delta; prefer the signed field |

JSON uses integer bytes. CLI/dashboard use 1,024-based KiB/MiB/GiB. Per-category
totals may overlap through hard links; only final scan/selection totals are
globally deduplicated. Intermediate progress is provisional. APFS clones,
snapshots, compression, open files and unrelated writes prevent attributing
the observed delta solely to this operation. Quarantine would not free payload
blocks; recovery is not yet exposed.

Warnings, `partial`, and `warnings_omitted` describe coverage, not an empty disk.
Missing compiled cache directories are normal; unreadable, changed, symlinked,
cross-device or bounded areas are not silently reported as inspected zeroes.
Only fully inspected candidates can be selected. Rejected/failed cleanup items
set partial outcomes; a directory may have lost some contents before failure.
“Old” means last modification, never last use. Recommendations are deterministic
policy explanations, not assertions that a user no longer needs a file.

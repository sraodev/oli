# Feature parity roadmap

Goal: an independently implemented, CLI-first Mac maintenance tool with an
original interface. CleanMyMac is a feature reference, not the name, artwork,
source code, or safety policy of this project. Full parity is **not implemented**.

## Reference and current coverage

This inventory was checked on August 30, 2026 against MacPaw's
[tool catalogue](https://macpaw.com/support/cleanmymac/knowledgebase/my-tools),
[Smart Care documentation](https://macpaw.com/support/cleanmymac/knowledgebase/smart-care),
[Space Lens documentation](https://macpaw.com/support/cleanmymac/knowledgebase/space-lens-results),
and [Cloud Cleanup documentation](https://macpaw.com/support/cleanmymac/knowledgebase/cloud-cleanup).
Coverage varies by CleanMyMac edition; this is a functional roadmap, not a
claim of equivalent protection or effectiveness.

| Feature family | Current implementation | Remaining work |
| --- | --- | --- |
| Combined care scan | Cache scan, explanations, risk/age selection, explicit apply | A unified report across independent inspection modules; no all-purpose health score |
| Junk cleanup | Old user caches/logs, Xcode build data, developer caches, separate Trash review | App-specific disposable-data rules; never broaden into blanket Library deletion |
| Disk visualization | Storage Atlas: selected personal-folder totals and top-level folder ranking | Recursive visual navigation and additional volumes |
| Large and old files | Metadata-only lists in CLI, JSON, and dashboard | Manual, recoverable actions after a separate safety design |
| Downloads | Read-only inspection; never auto-deleted | File preview and explicit review workflows |
| Duplicates and similar images | Not implemented | Content-hash confirmation, hard-link/clone awareness, cloud-placeholder exclusion; separate image similarity |
| Application management | User Applications folder size inspection only | Installed-app inventory, precise bundle ownership, reversible uninstall, leftovers review, trusted update sources |
| Login/background items and maintenance | Not implemented | Supported macOS APIs, read-only inventory first, explicit per-action permissions |
| Malware protection | Not implemented; no claim of antivirus protection | Maintained detection engine, update trust chain, false-positive handling, quarantine/restore, independent validation |
| Privacy and app permissions | Not implemented | App-specific schemas and supported OS permission controls; protect browser credentials and active sessions |
| Cloud cleanup | Not implemented | Provider authorization, local/cloud deletion distinction, retention and recovery rules |
| Background health monitoring | Not implemented | Explicit opt-in lifecycle, bounded resource use, meaningful alerts |

## Delivery order

1. **Storage discovery**: current milestone. A read-only, bounded Storage Atlas,
   separate from cleanup authorization and with no arbitrary-path API.
2. **Duplicates and app inventory**: read-only reports first; tests for identical
   bytes, changed files, hard links, sparse data, and application bundles.
3. **Recoverable actions**: user-selected file moves and app removal with
   ownership checks, a review manifest, and a tested restore path.
4. **System maintenance and privacy**: individual, documented actions; no
   unexplained “speed boost,” permission bypass, or blanket cache removal.
5. **Protection and cloud integrations**: separate threat/provider integrations
   with evidence of effectiveness and clear consent/recovery contracts.

## Storage Atlas acceptance criteria

- Human-readable and versioned JSON CLI output plus a distinct local dashboard.
- A compiled scope ID, never a client-provided path; Downloads is the default.
- Separate logical and allocated sizes; neither is labelled reclaimable.
- Hard-linked files counted once, symlinks not followed, filesystem boundaries
  skipped, directory identity checked after opening through `os.Root`.
- At most 200,000 entries, depth 64, 100 warnings, and 200 items per result list;
  CLI/API use a two-minute timeout and support cancellation.
- Permission errors and resource limits produce an explicitly partial report.
- “Old” is modification age, never a claim of last use or safe deletion.
- No content reads, deletion plan, or cleanup action for personal files.

The current milestone meets these criteria in local tests. It is not full
CleanMyMac feature parity, an antivirus product, or a public release of this
new functionality until separately published.

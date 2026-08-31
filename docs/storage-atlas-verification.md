# Storage Atlas integration verification

Issue: [#2](https://github.com/sraodev/mac-cleanup-studio/issues/2) (F01).
Verified on August 31, 2026.

## Scope and publication state

The Storage Atlas implementation was integrated at
[`9161262`](https://github.com/sraodev/mac-cleanup-studio/commit/9161262704eece2ea0778b25f756eff03b238ecd).
Its [source CI passed](https://github.com/sraodev/mac-cleanup-studio/actions/runs/33359243626).
This acceptance pass adds regression coverage; it does not implement the
separate F02–F07 features also mentioned in the issue body, expand cleanup
authority, or publish a binary release.

## Regression coverage

- Fixed default scope and all seven scope IDs; unrelated folders excluded.
- Inclusive size and modification-age thresholds, default limits, and stable
  ranking. Modification age is not last use.
- Hard links counted once; symlinks not followed; directory identity changes,
  permission failures, entry limits, depth/device boundaries and warning caps.
- Parsed CLI thresholds reaching JSON reports; human-readable partial warnings.
- Cancellation and expired deadlines producing no completed CLI report.
- Authenticated path-free API; partial reports preserved; two-minute operation
  deadline installed; request and server cancellation propagated.
- Exploration serialized against other dashboard operations; cancellation
  releases the operation lock; exploration creates no cleanup plan.

The entry-limit test uses a reduced internal bound. The device-boundary test
uses mismatched device metadata; it is not an external-drive compatibility test.
Deadline tests inspect the configured deadline and use expired contexts rather
than waiting two minutes. No production timeout setting was added for tests.

## Browser smoke checks

The browser skill was used against the embedded dashboard and real exploration
engine with temporary synthetic data, not personal folders. The temporary
service disabled cleanup and supplied synthetic disk capacity. One scope used
a delayed response to exercise cancellation without creating a large workload.

Verified:

- Authenticated load and Storage Atlas navigation/active state.
- Folder, large-file and old-file views, including a 120 MiB sparse fixture:
  logical size and allocated size remained distinct, not reclaimable totals.
- Empty-scope messaging and partial warnings for a symlinked scope.
- All-scopes review, with unavailable scopes reported as warnings.
- Progress, disabled scan/scope controls while exploring, stop messaging,
  server-side cancellation, restored controls and successful retry.
- No deletion controls inside Storage Atlas and no browser console warnings
  or errors during these flows; desktop layout visually inspected.

The temporary browser tab and server were stopped after verification. This is
a manual smoke check, not a newly installed browser-test framework.

## Commands and results

Environment: Go 1.25.5, macOS 26.5, Apple Silicon.

```text
$ go test -count=1 -race ./...
ok  github.com/sraodev/mac-cleanup-studio/cmd/mac-cleanup-studio  1.722s
ok  github.com/sraodev/mac-cleanup-studio/internal/app           3.277s
ok  github.com/sraodev/mac-cleanup-studio/internal/cleanup       3.823s
ok  github.com/sraodev/mac-cleanup-studio/internal/cli           2.202s
ok  github.com/sraodev/mac-cleanup-studio/internal/dashboard     2.753s
```

Focused checks are reproducible with:

```sh
go test -count=1 -race -run Explore -v ./cmd/mac-cleanup-studio ./internal/cleanup ./internal/app ./internal/dashboard
make check
node --check internal/dashboard/static/app.js
go mod tidy -diff
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/mac-cleanup-studio
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/mac-cleanup-studio
git diff --check
```

## Remaining limitations

Runtime/browser validation covers the listed Apple Silicon/macOS environment;
Intel is cross-build-verified, not hardware-tested here. No broader macOS
version matrix, mobile-layout audit, full-disk scan, actual external-volume
test, notarization or release installation was performed. The feature remains
read-only metadata inspection with bounded, potentially partial results.

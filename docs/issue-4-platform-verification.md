# Issue #4 — platform spike evidence

Status: **partial implementation; recovery commands remain unavailable**.
The user approved the design. The separate permission request to create/mount
a disposable APFS test image has not yet been answered. The real cross-volume
test is therefore skipped, and the platform-proof gate remains open.

Worktree: `mac-cleanup-studio-issue-4`, branch
`codex/issue-4-recovery-design`, base `a9f91004f271abd2c8599a9a826d3f966365b703`.
Issue #3's source/tests were copied into this worktree; the original checkout
was not changed. No commit, push, release, real cleanup, or volume mount occurred.

## Implemented

- `internal/recovery/move.go`: single-component names and already-open directory
  handles; distinguishes failed rename from moved-but-not-confirmed-durable.
- `move_darwin.go`: local writable APFS / same-device gates and
  `unix.RenameatxNp(..., RENAME_EXCL)`; no overwrite or copy/delete fallback.
- `move_other.go`: unsupported platforms fail closed.
- Pinned `golang.org/x/sys` v0.35.0 for the maintained Darwin adapter; CGO remains
  unnecessary. No custom assembly or shell-based movement.

This is a private platform primitive, not an authorization boundary. A future
caller must verify source/store ownership, ACLs, compiled roots, expected parent
identity and manifests. Holding an opened parent prevents symlink redirection;
it does not by itself reject a parent that was renamed after it was opened.

## Verified on macOS 26.5 / arm64, Go 1.25.5

- File and directory round trips retain payload contents, inode identity,
  permissions and modification time.
- Both file and directory sources refuse occupied file, empty/nonempty
  directory, symlink and dangling-symlink destinations; destination contents
  and identity remain intact.
- Concurrent destination races yield one winner; the other source survives.
- Replacing the destination parent's old pathname with an external symlink
  does not redirect movement away from the opened original directory.
- Hard links and extended attributes survive. Writing through a remaining hard
  link changes the moved payload, demonstrating why future restore/purge must
  revalidate it. Moving a symlink leaves its target untouched.
- A directory-sync error after rename reports `Moved=true, Durable=false` and
  does not rollback or retry automatically. Both directory syncs are attempted.
- Eight subprocess-kill barriers pass: prepared write/sync, before move,
  after rename, source sync, destination sync, committed write/sync. A fresh
  process observes exactly one intact payload and the expected journal state.

The crash harness is test-only, not the bounded production journal. These are
process termination tests, not sudden-power-loss or hardware durability proof.
The helper subprocess test intentionally skips when invoked without its fixture
environment; its eight child runs are exercised by the parent test.

## Literal check results

`make check`:

```text
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
ok  github.com/sraodev/mac-cleanup-studio/cmd/mac-cleanup-studio  1.704s
ok  github.com/sraodev/mac-cleanup-studio/internal/app           4.226s
ok  github.com/sraodev/mac-cleanup-studio/internal/cleanup       2.219s
ok  github.com/sraodev/mac-cleanup-studio/internal/cli           3.158s
ok  github.com/sraodev/mac-cleanup-studio/internal/dashboard     2.683s
ok  github.com/sraodev/mac-cleanup-studio/internal/recovery      12.289s
go build -trimpath -o bin/mac-cleanup-studio ./cmd/mac-cleanup-studio
```

Fresh `go test -count=1 -race ./...`:

```text
ok  github.com/sraodev/mac-cleanup-studio/cmd/mac-cleanup-studio  1.560s
ok  github.com/sraodev/mac-cleanup-studio/internal/app           1.907s
ok  github.com/sraodev/mac-cleanup-studio/internal/cleanup       1.304s
ok  github.com/sraodev/mac-cleanup-studio/internal/cli           1.678s
ok  github.com/sraodev/mac-cleanup-studio/internal/dashboard     2.261s
ok  github.com/sraodev/mac-cleanup-studio/internal/recovery      10.661s
```

The focused suite explicitly reports the unrun gate:

```text
=== RUN   TestExclusiveMoveCrossVolume
    requires operator-approved disposable APFS volume; not simulated
--- SKIP: TestExclusiveMoveCrossVolume (0.00s)
```

These commands also exited 0:

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o /dev/null ./internal/recovery
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go test -c -o /dev/null ./internal/recovery
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/mac-cleanup-studio
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/mac-cleanup-studio
node --check internal/dashboard/static/app.js
go mod tidy -diff
```

The adapter test binaries are cross-built explicitly because the product CLI
does not import recovery yet. Intel is **not** runtime-verified.

## Remaining gate and implementation

1. With operator consent only, create/mount one disposable APFS image. Set
   `MCS_RECOVERY_TEST_OTHER_VOLUME` to its mountpoint and run the existing
   cross-volume test. Verify no payload movement, then unmount and remove the
   exact test image. Never point this test at personal data or an unapproved disk.
2. Build store permission/ACL checks, bounded records and admission, durable
   journal, restart reconciliation and explicit record forgetting.
3. Implement quarantine, collision-safe restore and explicit purge through
   current private scan/record plans; retain partial and changed outcomes.
4. Wire canonical CLI/JSON plus authenticated dashboard and full lifecycle E2E.
   Existing copied issue #3 E2E tests passing is not recovery lifecycle evidence.

No browser recovery E2E has run: there is no recovery UI yet. No user cache or
personal file was scanned or moved by the new adapter; all probe payloads were
temporary test fixtures.

## References

- [Approved design](issue-4-recovery-design.md)
- [Reviewer roadmap](diagrams/issue-4-recovery-roadmap.svg)
- [Go x/sys v0.35.0 Darwin adapter source](https://go.googlesource.com/sys/+/refs/tags/v0.35.0/unix/syscall_darwin.go)
- [Go rename semantics](https://pkg.go.dev/os#Rename)

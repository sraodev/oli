# Repository readiness — verification

Date: 2026-09-06. Environment: macOS 26.5, arm64, Go 1.25.5.
Runtime baseline: `c74b5b9966b4a787a9d40420c60c284870600030` (merged rename).
No changes to `cmd/`, `internal/`, `go.mod` or `Makefile` are part of this PR.
The PR identifies the final documentation/workflow commit separately.

## Local runtime regression checks

`make check` passed formatting, vet, race tests and the `bin/oli` build. Its
race results used cached successes; the independent command below reran all
package tests without caching:

```text
$ go test -count=1 -race ./...
ok  github.com/sraodev/oli/cmd/oli             3.671s
ok  github.com/sraodev/oli/internal/app        1.747s
ok  github.com/sraodev/oli/internal/cleanup    3.430s
ok  github.com/sraodev/oli/internal/cli        2.831s
ok  github.com/sraodev/oli/internal/dashboard  2.272s

$ go test -count=1 -race ./cmd/oli -run TestOliBinaryE2E -v
=== RUN   TestOliBinaryE2E
--- PASS: TestOliBinaryE2E (1.31s)
PASS
ok  github.com/sraodev/oli/cmd/oli 3.122s
```

The E2E builds the actual executable and exercises help/version, capabilities,
scan/recommend/dry run, incomplete confirmation refusal and confirmed deletion
of one synthetic cache. It asserts fixture preservation on read-only/refusal
paths. No personal files were used for destructive testing. Existing regressions
cover cancellation, changed candidates, partial reports and HTTP safety gates.

`node --check internal/dashboard/static/app.js` and `git diff --check` exited 0.
No new browser runtime was introduced or changed, so a new product-browser E2E
run is not claimed. The prior rename's manual smoke remains historical evidence,
not a newly rerun browser test. Fixture binary E2E was rerun for this PR.

## Documentation, workflow and visual checks

- Parse the two workflows, Dependabot config and three issue-template YAML files
  with Ruby's YAML parser: all six passed. The system Ruby emitted an unrelated
  optional `ffi` extension warning; parsing and assertions still exited 0.
- Assert every external action uses a full 40-character SHA, every checkout
  disables credential persistence and workflow default contents permission is
  read-only: passed. This is static validation, not hosted workflow execution.
- Required CI job names and tag triggers are unchanged. The existing release
  job alone retains write permission. No tag or release workflow was executed.
- Validate local Markdown targets and heading anchors, and assert unchanged
  runtime/build source against main before push. Final counts are in the PR.
- Parse/format four original SVGs with `xmllint`; render using the existing
  temporary Sharp runtime, with no repository dependency; visually inspect all
  four PNGs for clipping, readability and status/safety labels.
- Diagrams: roadmap and architecture 1440 × 850; sequence 1440 × 945.
  Social card: 1280 × 640, 62,175 bytes (below 1 MB). SVGs remain editable.
- Check new public material for copied product names: no matches. The existing
  v1 protocol identifier and historical verification records are intentionally
  preserved rather than rewritten as branding.

The temporary render/check tooling is not part of the runtime or CI dependency
graph. SVG is the canonical diagram source; no Mermaid parser result is claimed.

## Live settings readback

At this review's baseline, main protection was enabled with strict quality and
both Darwin build checks, GitHub Actions check ownership, admin enforcement,
resolved conversations, and force-push/deletion disabled. Zero independent
approvals are required for the sole maintainer. These settings were established
before this PR and are not represented as changes implemented by it.

## Remaining gates

- Required CI must pass on the pushed PR head; local checks do not replace it.
- Maintainer review and explicit merge authorization are required. No merge is
  performed as part of creating this PR.
- After merge, verify default-branch forms, documentation and community profile.
- Social-preview upload is separate; generating a PNG does not configure GitHub.
- Intel hardware, actual downloaded-release install/update/uninstall, signing,
  notarization and release publication remain #8 acceptance work.
- No Linux, package-manager, agent lifecycle, low-space automation or native
  capability is delivered here. Those tasks stay open.

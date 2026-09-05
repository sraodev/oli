# Contributing

Thank you for helping make Oli safer and clearer.

## Before opening a pull request

Use macOS and the Go version declared in `go.mod`. Format the code, then run the
core CI checks:

```sh
gofmt -w .
go vet ./...
go test -race ./...
mkdir -p ./bin
go build -trimpath -o ./bin/oli ./cmd/oli
```

Or run `make check`.

Keep changes focused. This project targets one standard-library Go binary, so
reuse existing packages before proposing a dependency, service, installer, or
build framework.

## Safety-sensitive changes

A cleanup rule is a destructive feature. A proposal should identify:

- the exact user-scoped root it scans;
- why the data is safe to regenerate;
- its risk/action classification and minimum age;
- its explicit exclusions;
- what happens on unreadable files, symlinks, path escapes, and concurrent
  filesystem changes;
- focused tests for dry-run behavior and deletion boundaries.

Do not broaden a rule into system paths, `~/Downloads`, Docker data, backups,
or archives. Backups and archives can be review-only findings, never automatic
cleanup targets. Preserve the requirement that CLI deletion needs both
`--apply` and `--yes`, and that dashboard deletion requires its completed-scan
selection plus the exact confirmation word.

Changes affecting path containment, deletion, dashboard binding or tokens, or
JSON output should include a regression test. Report a security flaw privately
as described in [SECURITY.md](SECURITY.md), not in a public issue.

The CLI JSON contract is agent-agnostic and versioned. Keep recommendations
deterministic and explainable, preserve the `schema_version` field, and do not
add model-specific deletion authority or arbitrary-path inputs.

## Pull requests

In the description, include:

- the user-visible problem and the smallest chosen solution;
- a checked-in scope document for multi-part changes: included/excluded work,
  ordered review links, invariants, failure behavior and remaining gates;
- the macOS version and architecture used for testing;
- the exact verification commands and their output summary;
- before/after screenshots for dashboard changes;
- any change to the documented scan, review, or clean contract.

Every PR must include a reviewer roadmap diagram: shipped behavior, the PR's
delta, review order or dependencies, remaining work, and the hard safety
boundary. Use verified issue/commit references and text status labels alongside
colors. Keep small changes' diagrams small. Include an editable source and
visually inspect the rendered result; see the
[Storage Atlas example](docs/diagrams/pr-33-reviewer-roadmap.svg).

For architecture or workflow changes, attach sequence and architecture diagrams
alongside the roadmap. Keep editable sources with rendered previews and link
the checked-in documents from the PR body. The
[repository-readiness review package](docs/reviews/repository-readiness-scope.md)
shows this structure without treating planned work as implemented.

Run relevant end-to-end flows for every implementation change using synthetic
fixtures, never personal data for destructive testing. Cover the relevant CLI,
API, and UI paths, including errors and cancellation. Record exact actions,
environment, and results; distinguish manual smoke checks from automated E2E
and unit/integration tests. If E2E is blocked, report the missing coverage and
blocker explicitly rather than claiming completion.

Documentation and tests should describe behavior, not an unimplemented future
design. Avoid unrelated refactors in the same pull request.

Release artifacts are produced only by the tagged GitHub Actions workflow.
Do not upload locally built replacement binaries to an existing release; tag
and source history must remain the reproducible release boundary.

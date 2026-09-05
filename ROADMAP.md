# Oli roadmap

[Issue #1](https://github.com/sraodev/oli/issues/1) is the canonical prioritized
inventory and dependency map. Avoid maintaining a second set of completion
checkboxes here. Open issues are acceptance work, not advertised functionality.

## Delivery boundaries

| Stage | Coverage | Gate |
| --- | --- | --- |
| Merged source | Go CLI/JSON, local dashboard, read-only Atlas, Oli rename #43 | Not a published binary |
| P0 | [#2–8 trust and delivery](https://github.com/sraodev/oli/issues?q=is%3Aissue%20is%3Aopen%20label%3AP0) | Reconcile unmerged work and actual acceptance |
| P1 | TUI #22, summary #35, inspection, identity #51, advisory hooks #53 | Same engine/contracts; no automatic deletion authority |
| P2 | Targeted caches, history, local web, Linux #49, policy #54, low-space #58 | Platform tests, explicit policy and consent |
| P3 | System/security/cloud/native work and PowerShell #48 | Separate feasibility and permission review |

## Distribution

[#8](https://github.com/sraodev/oli/issues/8) owns release validation.
Separate channel tasks cover [curl #44](https://github.com/sraodev/oli/issues/44),
[Homebrew #45](https://github.com/sraodev/oli/issues/45),
[mise #46](https://github.com/sraodev/oli/issues/46),
[Bun/npm #47](https://github.com/sraodev/oli/issues/47),
[PowerShell #48](https://github.com/sraodev/oli/issues/48) and
[installation docs #50](https://github.com/sraodev/oli/issues/50).
Optional channels do not all block the first release. Linux must pass
[#49](https://github.com/sraodev/oli/issues/49) before being advertised as supported.

## Agent housekeeping

The direction is to help people and agents understand storage pressure and
maintain reliable workspaces. First comes
[bounded advisory integration #53](https://github.com/sraodev/oli/issues/53),
then [explainable preview policies #54](https://github.com/sraodev/oli/issues/54)
and [low-space housekeeping #58](https://github.com/sraodev/oli/issues/58).
[Benchmarks #55](https://github.com/sraodev/oli/issues/55) must establish overhead
and outcomes before speed or productivity claims.

Low space, an old timestamp or an agent request is not deletion authority.
Downloads, personal data, active projects and agent runtimes stay protected.
No automatic RAM purge or process termination is planned as a generic cleanup.

## Contribute a focused change

Start with [byte-format tests #56](https://github.com/sraodev/oli/issues/56) or
[a fixture-verified JSON example #57](https://github.com/sraodev/oli/issues/57).
See [help wanted](https://github.com/sraodev/oli/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22help%20wanted%22)
and [Contributing](CONTRIBUTING.md). Discuss safety-sensitive scope before coding.

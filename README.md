# Oli - Open Lifecycle Intelligence 

```
          ,\
          \\\,_       ___  _     ___
           \` ,\     / _ \| |   |_ _|
      __,.-" =__)    | (_) | |__ | |
    ."        )      \___/|____|___|
,_/   ,    \/\_
\_|    )_-\ \_-`     Open Lifecycle Intelligence
   `-----` `--`      preview-first macOS cleanup for people,
                     scripts, and local agents
                     github.com/sraodev/oli
```

Open Lifecycle Intelligence — preview-first macOS cleanup for people, scripts, and local agents.

[![CI](https://github.com/sraodev/oli/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/sraodev/oli/actions/workflows/ci.yml)
[![MIT license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Get started](#try-oli) · [User guide](docs/guides/usage.md) · [Agent contract](docs/agent-interface.md) · [Roadmap](ROADMAP.md) · [Contribute](CONTRIBUTING.md)

![Oli: make room, keep control. Local macOS cleanup with CLI and JSON, explicit review, and no cloud account.](docs/assets/oli-social.png)

## What is Oli?

Oli (**Open Lifecycle Intelligence**) is a local-first housekeeping project for
developer workspaces and AI-agent workflows. Its goal is to make resource use
understandable: what is taking space, what can be regenerated, what must be
protected, and what an explicitly approved cleanup actually changed.

Today, Oli addresses **disk-space pressure on macOS** through a Go CLI,
versioned JSON and a local dashboard. It is intended for developers and local
automation—not a centralized enterprise manager, cloud data-governance platform
or replacement for the operating system's memory management.

The next stage is lifecycle-aware housekeeping: bounded agent integration,
workspace policies and low-space recommendations that protect active work.
Those capabilities are [planned](ROADMAP.md#agent-housekeeping), not enabled
autonomy. Reliable workspaces are the goal; faster agents must be demonstrated
with measurements, not assumed from deleting caches.

## Know what takes space. Decide what goes.

Caches and build leftovers grow while you work. Oli measures eligible data,
explains the rules, and lets you review the cleanup before applying it.
It runs locally as one Go binary, with an optional browser dashboard.

- **See the numbers:** logical bytes, allocated-byte estimates, and observed free-space change are separate.
- **Inspect personal folders:** Storage Atlas ranks folder sizes and large/old files without deleting them.
- **Keep control:** compiled user-scoped rules, age gates, fresh-plan validation, and explicit confirmation.
- **Call it from your tools:** versioned JSON and exit codes; no model-specific SDK or hosted service.

> [!WARNING]
> Oli is early-stage software. Applied cleanup permanently deletes files; it
> does **not** move them to Trash or provide recovery. Review exact paths and
> read the [safety model](docs/safety.md) before applying anything.

## Try Oli

**Source build first.** No downloadable Oli release, supported curl installer,
or package-manager installation is currently published. Track distribution in
[issue #8](https://github.com/sraodev/oli/issues/8).

Requires macOS and the Go version declared in [go.mod](go.mod).
From a reviewed checkout:

```sh
git clone https://github.com/sraodev/oli.git
cd oli
make check
./bin/oli help
./bin/oli scan --profile safe
```

The scan is read-only. To inspect personal files or open the local dashboard:

```sh
./bin/oli explore --scope downloads
./bin/oli dashboard
```

No account, subscription, telemetry, or `sudo`. The dashboard uses an
authenticated loopback connection. Do not share its launch URL.

## From inspection to action

```sh
./bin/oli capabilities --json                     # discover the contract
./bin/oli recommend --profile safe --json           # explain eligible rules
./bin/oli clean --rules user-caches,user-logs       # dry run only
```

Deletion is a separate decision. The [user guide](docs/guides/usage.md) explains
confirmation, fresh scans, partial failures, interruption, and estimates.
A dry run does not authorize a later action.

## What works today?

| Area | Current source behavior | Boundary |
| --- | --- | --- |
| Cleanup | Old user caches/logs, Xcode DerivedData, reviewed developer caches, separate Trash rule | Permanent deletion, explicit confirmation |
| Storage Atlas | Fixed personal-folder scopes, folder ranking, large/old-file lists | Read-only metadata; not full-disk exploration |
| Automation | CLI, versioned JSON, deterministic recommendations | No autonomous agent hook or lifecycle daemon |
| Dashboard | Local scan/review, rule selection, Atlas, per-session activity | Not a hosted service or native app |
| Recovery, TUI, app uninstall, cloud, memory management | Planned or separate unmerged work | Not available in this source branch |

Oli does not purge RAM, stop your processes, delete personal files automatically,
or claim to make every agent faster. Its **agent-aware housekeeper direction is
a roadmap**, not shipped autonomy. See [the feature inventory](ROADMAP.md).

## Safety is part of the interface

- Cleanup stays within allowlisted locations in the current user's home.
- Downloads, Docker data, backups and archives are not automatic cleanup targets.
- Personal-file inspection never creates deletion authority.
- Permission errors and incomplete scans are reported, not silently bypassed.
- APFS clones, snapshots and concurrent writes mean estimated bytes are not a promise of reclaimed capacity.

**Disk space is not RAM.** Oli's current cleanup addresses storage; memory-pressure
inspection and lifecycle-aware process/resource controls require separate work.

## Help build Oli

Start with [good first issues](https://github.com/sraodev/oli/contribute) or
[help wanted](https://github.com/sraodev/oli/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22help%20wanted%22).
Tests, accessibility feedback, readable docs, and reproducible Mac compatibility
reports are valuable contributions—not just new cleanup rules.

Read [Contributing](CONTRIBUTING.md) for setup, small-PR expectations, synthetic
fixtures, and required reviewer diagrams. Ask questions through [Support](SUPPORT.md);
report vulnerabilities [privately](https://github.com/sraodev/oli/security/advisories/new).

If Oli is useful to you, a star helps others discover it. A concrete bug report
or tested contribution helps make it better.

## Find your way around

| Path | Purpose |
| --- | --- |
| [cmd/oli](cmd/oli) | Executable, human/JSON output, binary E2E |
| [internal/cleanup](internal/cleanup) | Scan and deletion safety engine |
| [internal/app](internal/app) | Profiles, recommendations, dashboard orchestration |
| [internal/cli](internal/cli) | Argument parsing and usage |
| [internal/dashboard](internal/dashboard) | Loopback HTTP adapter and embedded UI |
| [docs](docs/README.md) | User guides, contracts, development and maintainer references |

Upgrading from the previous project name? Use the `oli` executable and
`github.com/sraodev/oli` module. The existing v1 wire identifier is retained for
[agent compatibility](docs/agent-interface.md#rename-compatibility); no data or
installed binary is migrated automatically.

## License

[MIT](LICENSE). See the [Code of Conduct](CODE_OF_CONDUCT.md) for community expectations.

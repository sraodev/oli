# Cleanup coverage and boundaries

This is a source capability inventory, not permission to clean this Mac or a
promise of complete app/version coverage. Discover exact compiled roots with
`oli capabilities --json`. Scans are read-only; `clean` is a preview unless both
`--apply --yes` are supplied. Deletion is permanent. Close affected apps/tools
before reviewing a cleanup; modification age is not evidence that data is unused.

| Requested operation | Current source behavior | Remaining boundary |
| --- | --- | --- |
| Trash on all mounted volumes and main disk | `trash`: current user's `~/.Trash`, explicitly selected | All-volume emptying deferred to #75; never other users' Trash |
| System logs | `user-logs`: old `~/Library/Logs` children | System-wide paths excluded; no elevation |
| Adobe caches | `app-specific-caches`: media-cache files inventory | Scan-only; no credentials, projects or presets |
| iOS applications | `ios-app-packages`: legacy iTunes Mobile Applications inventory | Scan-only; packages may be irreplaceable; #73 |
| iOS device backups | `ios-device-backups` inventory | Scan-only; recovery required before removal |
| Xcode DerivedData and archives | Age-gated `xcode-derived-data`; `xcode-archives` inventory | Archives never cleaned; #73 |
| Reset simulators | Not implemented | Exact-device consent and recovery design; #74; no erase-all |
| Homebrew cache | Default `~/Library/Caches` coverage | Custom cache paths, installed formulae and upgrades excluded; #16 |
| Old Gems | Not implemented | Dependency-aware selection; no global gem cleanup; #72 |
| Dangling Docker images | Not implemented | Exact-ID read-only inventory first; #71 |
| Inactive memory purge | Intentionally excluded | Storage is not RAM; no forced purge or speed claims |
| pip cache | Default `~/Library/Caches` coverage | Custom/XDG cache locations not discovered |
| Pyenv/virtualenv cache | `developer-caches`: `.pyenv/cache` downloads | Installed Pythons and virtual environments preserved |
| npm cache | `developer-caches`: `.npm/_cacache` | Config, credentials, global packages and projects excluded |
| Yarn cache | Default `~/Library/Caches` coverage | Project-local/custom cache paths excluded |
| Docker images/stopped containers | Not implemented | Stopped containers may contain unique data; #71; no volumes or prune-all |
| CocoaPods cache | Default `~/Library/Caches` coverage | Installed Pods, repos and project files excluded |
| Composer cache | Default `~/Library/Caches` coverage | No vendor directories or credential/config deletion |
| Dropbox cache | Legacy `~/Dropbox/.dropbox.cache` inventory | Scan-only; modern File Provider and sync semantics unresolved |
| PhpStorm logs | Default `~/Library/Logs` coverage | Settings, indexes and project data excluded |
| Minecraft logs/cache | `app-specific-logs`: logs and crash-reports | Cache removal not implemented; worlds, assets and mods preserved |
| Steam logs/cache | `app-specific-logs`: logs; app/depot cache inventory | Cache scan-only; games, downloads, saves, accounts untouched |
| Lunar Client logs/cache | `app-specific-logs`: top-level logs; game/launcher cache inventory | Cache scan-only; offline profile data excluded |
| Teams logs/cache | Default `~/Library/Logs` and `~/Library/Caches` coverage only | Container/version-specific locations need #70; no full app-support deletion |
| Wget logs and hosts | Not implemented | Preserve `.wget-hsts` security state; not a hosts cache; #70 |
| Cacher logs | `app-specific-logs`: `.cacher/logs` | Snippets, accounts and configuration preserved |
| Android caches | `developer-caches`: `.android/cache` only | SDKs, AVDs, keys and emulator data preserved |
| Gradle caches | `developer-caches`: `.gradle/caches` | No daemons, wrapper installations, settings or project outputs |
| Kite logs | `app-specific-logs`: `.kite/logs` | No application settings or blanket `.kite` removal |
| Go module cache | `developer-caches`: `~/go/pkg/mod/cache` downloads | Not the whole module tree; custom GOPATH/GOMODCACHE and build cache excluded |
| Poetry cache | Default `~/Library/Caches/pypoetry` explicitly protected | May contain environments; cache-only provider remains #16 |

Generic cache/log rules have a 30-day modification gate, DerivedData seven days.
Developer and app-specific log rules require manual selection (not the safe/auto
profile) and a 30-day gate. A changed or unreadable candidate cannot be assumed
disposable. Excluded Poetry contents are neither scanned nor counted as
reclaimable; the warning and `excluded_children` capability field disclose this.
Custom environment/config paths are not followed. Missing known roots are skipped;
this does not prove an application has no other caches.

## Preview the expanded catalog

```sh
oli scan --rules developer-caches,app-specific-logs --json
oli clean --rules developer-caches,app-specific-logs   # preview only
oli scan --rules app-specific-caches,ios-app-packages,ios-device-backups,xcode-archives --json
```

There is no “delete everything above” operation. Restore, per-item selection,
app-running checks, provider-specific recovery, system paths, simulator/Docker
operations and complete third-party layout coverage remain separate issues.

## Path evidence

Default locations are not guarantees across application versions. See
[pip caching](https://pip.pypa.io/en/stable/topics/caching/),
[Poetry environment placement](https://python-poetry.org/docs/configuration/#virtualenvspath),
[Android directory variables](https://developer.android.com/tools/variables), and
[pyenv environment management](https://github.com/pyenv/pyenv-virtualenv#uninstalling-virtualenv).
The broader app-cache inventory is deliberately read-only pending validation.

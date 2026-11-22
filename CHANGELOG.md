# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [Unreleased]
### Added
- Health tooltip on each app showing latency, last error, and recent uptime %.
- Optional uptime history bar on each app card (last 30 checks, kept in memory);
  enable with `settings.health_check.show_history: true`.
- Disk usage widget in the metrics bar (`HOMELAND_DISK_PATH` to pick the mount).
- Optional webhook alerts when an app goes down or recovers (`settings.notify`).
- Build version in the footer and at `GET /api/version`.

### Changed
- Redesigned UI: single sticky header with live metrics, greeting with date and
  clock, "X of Y services online" summary, compact app tiles, minimal group
  headers, tighter calendar and bookmarks, softer dark theme.
- Metrics bar no longer overlaps on narrow screens; items wrap and stay centered.

### Fixed
- Docker-discovered apps are now health-checked (they always showed "unknown").
- Docker-discovered groups keep a stable order instead of shuffling on refresh.
- Auth settings and health-check interval/timeout now apply on hot reload.
- `POST /api/reload` re-reads the config file instead of only restarting checks.
- Cross-site POST requests are rejected (CSRF protection).
- Icon names containing paths (e.g. `../`) are rejected.
- `.gitignore` patterns were prefixed with `+` and never matched; generated CSS,
  `.fiber.gz` files and the icon cache are no longer tracked.
- Docker image now embeds the version passed via `--build-arg VERSION`.
- Reload button now refreshes the page after reloading the config and shows
  an error when the config is invalid (it previously did nothing visible).

## [v2.0.0] 04-07-2026
### Added
- Added a **Bookmarks** section.

### Changed
- Moved the **Metrics** display from the right sidebar to below the navigation bar.

### Fixed
- Resolved [#2](https://github.com/abhixops/homeland/issues/2).
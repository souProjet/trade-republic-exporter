# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-10-09

### Added

- French interface: commands, help, settings, dashboard and messages.
  `interface.language` picks `en`, `fr` or `auto`, which follows the system
  language.

### Changed

- The settings screen shows every setting on one page, grouped by section,
  with its current value, where it comes from and an explanation, instead of a
  page-by-page wizard. Choices switch with the arrow keys, datasets are a
  checklist, and unsaved changes are counted and confirmed before quitting.
- `config reset` asks with a plain y/N prompt.

### Removed

- The `huh` dependency.

## [0.2.0] - 2026-10-09

### Added

- Full-screen dashboard: sign-in steps and accounts, datasets with live
  progress bars, a scrollable log, a two-factor prompt with a countdown and an
  SMS shortcut, and `o` to open the output folder. It follows the terminal's
  light or dark background and adapts to narrow windows.
- Settings editor (`tr-export config`), opened automatically on the first run.
- `config show`, `get`, `set`, `unset`, `edit`, `path` and `reset` commands,
  with shell completion for setting names and values.
- PIN storage in the system keychain: macOS Keychain, Windows Credential
  Manager, Secret Service on Linux. A PIN is refused on the command line,
  where it would stay in the shell history.
- `export.datasets` and `--datasets` to export a subset.
- `standard` CSV dialect (comma, decimal point, ISO 8601, no BOM) next to the
  `european` one.
- `--demo` to preview the interface with sample data, without signing in.
- Styled help and errors, `--version`, release binaries for macOS, Linux and
  Windows.

### Changed

- Settings moved from `./config.ini` to `~/.config/trade-republic-exporter/`,
  with new section names. A v0.1 file in the working directory is imported on
  the first run and its PIN moved to the keychain.
- The generated device identity is saved after the first login.
- The binary is installed from `./cmd/tr-export` and named `tr-export`
  everywhere.
- Piped output, `--plain` and `--quiet` keep the line-by-line output of 0.1.

### Fixed

- `timelineDetailV2` parsing no longer assumes every section carries a list of
  rows. Object-shaped sections such as the header are skipped, nested text
  wrappers are unwrapped, and an unknown shape is kept as compact JSON instead
  of failing the whole event. Enrichment used to fail on every transaction.
- Detail columns are now named `detail.<section>.<field>`, so two sections
  cannot overwrite each other.
- Transaction enrichment gives up after five consecutive failures instead of
  sending one doomed request per transaction.

## [0.1.0] - 2026-10-09

First public release.

### Added

- Export of every account reported by `accountPairs`: the default securities
  account, a French PEA, and any other product type, each tagged on the rows it
  owns.
- Per-account holdings through `compactPortfolioByType`, crypto included as one
  of the returned categories, and per-account cash balances through `cash`.
- Customer-wide datasets: transactions, activity log, available cash, savings
  plans and open orders.
- Optional transaction enrichment through `timelineDetailV2` with `-details`.
- CSV output tuned for spreadsheets in European locales, and JSON output for
  machine processing.
- Terminal interface with per-dataset progress, an account table, a file
  summary, `NO_COLOR` support and a `-quiet` mode.
- Credentials from an INI file or from `TR_PHONE_NUMBER` and `TR_PIN`.
- Two-factor login with an SMS fallback.

[Unreleased]: https://github.com/souProjet/trade-republic-exporter/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/souProjet/trade-republic-exporter/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/souProjet/trade-republic-exporter/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/souProjet/trade-republic-exporter/releases/tag/v0.1.0

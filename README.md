# trade-republic-exporter

[![CI](https://github.com/souProjet/trade-republic-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/souProjet/trade-republic-exporter/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/souProjet/trade-republic-exporter.svg)](https://pkg.go.dev/github.com/souProjet/trade-republic-exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/souProjet/trade-republic-exporter)](https://goreportcard.com/report/github.com/souProjet/trade-republic-exporter)
[![Release](https://img.shields.io/github/v/release/souProjet/trade-republic-exporter)](https://github.com/souProjet/trade-republic-exporter/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**English** | [Français](README.fr.md)

Export your Trade Republic data to CSV or JSON from a full-screen terminal
dashboard: every account (securities account, French PEA, and whatever product
type Trade Republic reports next), the holdings in each one including crypto,
cash balances, transactions, the activity log, savings plans and open orders.

<img src="docs/dashboard.svg" alt="Export dashboard: session and accounts on the left, datasets with progress on the right, log at the bottom" width="900">

> [!WARNING]
> Unofficial tool. It drives the private API of the Trade Republic web app,
> which may break at any time and may conflict with the Trade Republic terms of
> service. Use it on your own account, at your own risk.

## Install

Download a binary for macOS, Linux or Windows from the
[latest release](https://github.com/souProjet/trade-republic-exporter/releases/latest),
or install with Go 1.25 or newer:

```bash
go install github.com/souProjet/trade-republic-exporter/cmd/tr-export@latest
```

Chrome or Chromium must be installed: the login goes through an AWS WAF
challenge that only a real browser can solve.

## Quick start

```bash
tr-export --demo   # preview the interface with sample data, no login
tr-export          # first run opens the settings editor, then exports
```

The first run asks for your phone number and PIN. The PIN goes to the system
keychain (macOS Keychain, Windows Credential Manager, Secret Service on Linux),
never to a file. Every later run signs in, asks for the two-factor code and
exports:

<img src="docs/two-factor.svg" alt="Two-factor prompt with a countdown, over the dashboard" width="900">

## Commands

| Command | What it does |
|---------|--------------|
| `tr-export` | Run an export with the saved settings |
| `tr-export config` | Open the settings screen |
| `tr-export config show` | List every setting, its value and where it comes from |
| `tr-export config get KEY` | Print one setting, for scripts |
| `tr-export config set KEY VALUE` | Change one setting |
| `tr-export config set account.pin` | Store the PIN in the keychain, typed without echo |
| `tr-export config unset KEY` | Go back to the default |
| `tr-export config edit` | Open the file in `$VISUAL` or `$EDITOR` |
| `tr-export config path` | Print where the file lives |
| `tr-export config reset` | Delete the settings and the stored PIN |
| `tr-export completion SHELL` | Shell completion for bash, zsh, fish or PowerShell, settings and values included |

The settings screen lists every setting on one page, grouped by account,
export, data and interface, with where each value comes from and an
explanation of the focused one. `↑↓` to move, `←→` to switch a choice,
`enter` to edit a text or pick datasets, `ctrl+s` to save.

The interface speaks English and French: `tr-export config set
interface.language fr`, or the Language setting on the screen. By default it
follows the system language.

<img src="docs/settings.svg" alt="Settings screen: every setting grouped by section, the focused one explained below" width="820">

### Export flags

Flags override the saved settings for one run.

| Flag | Description |
|------|-------------|
| `-f, --format csv\|json` | Output format |
| `--dialect european\|standard` | CSV flavor, see below |
| `-o, --out DIR` | Output directory |
| `--datasets a,b,c` | Only these datasets |
| `-d, --details` | Enrich every transaction with fees, quantities and venue (slower) |
| `--plain` | Line-by-line output instead of the dashboard |
| `-q, --quiet` | Only warnings and errors |
| `--demo` | Sample data, no login, nothing written |
| `--config FILE` | Use another settings file |

## Settings

Settings live in `~/.config/trade-republic-exporter/config.ini`
(`$XDG_CONFIG_HOME` is honored; `%AppData%` on Windows), written with
owner-only permissions. `tr-export config show` tells where each value comes
from: environment, file, keychain or default.

| Key | Default | Description |
|-----|---------|-------------|
| `account.phone_number` | | International format, for example `+33612345678` |
| `account.pin` | | Kept in the system keychain, never in the file |
| `account.device_info` | generated | Device identity, saved after the first login |
| `export.format` | `csv` | `csv` or `json` |
| `export.csv_dialect` | `european` | `european` or `standard` |
| `export.output_dir` | `out` | Relative to the working directory unless absolute; `~` works |
| `export.datasets` | `all` | Comma-separated list, see below |
| `export.details` | `false` | Fetch the detail view of every transaction |
| `interface.mode` | `auto` | `auto`, `fullscreen` or `plain` |
| `interface.language` | `auto` | `auto`, `en` or `fr`; `auto` follows the system language |

Environment variables win over the file and the keychain, for scheduled jobs
or CI: `TR_PHONE_NUMBER`, `TR_PIN`, `TR_DEVICE_INFO`, and `TR_WAF_TOKEN` to
reuse a WAF token instead of starting Chrome.

## What gets exported

| Dataset | Content | Subscription |
|---------|---------|--------------|
| `accounts` | One row per account pair, with its product type, securities and cash account numbers | `accountPairs` |
| `positions` | Holdings of every securities account, one row per position, tagged with the owning account. Crypto is one of the categories | `compactPortfolioByType` |
| `cash` | Cash balance of each account | `cash` |
| `available_cash` | Cash available for trading, net of funds reserved by open orders | `availableCash` |
| `transactions` | Full transaction timeline, paged until exhausted | `timelineTransactions` |
| `activity_log` | Logins, documents, account changes | `timelineActivityLog` |
| `savings_plans` | Recurring investment plans | `savingsPlans` |
| `orders` | Open orders | `orders` |

With details on, every transaction gets columns named
`detail.<section>.<field>` from its detail view.

A subscription the API rejects is shown as a failed dataset and the run
continues with the others, with a non-zero exit code at the end. An error is
never written out as an empty file, so a rejected portfolio request cannot be
mistaken for an empty portfolio.

### CSV dialects

| Dialect | Separator | Decimal | Timestamps | BOM | For |
|---------|-----------|---------|------------|-----|-----|
| `european` | `;` | `12,50` | `31/12/2026 18:30` local time | yes | Excel, Numbers, LibreOffice in European locales |
| `standard` | `,` | `12.50` | `2026-12-31T18:30:00+01:00` | no | English locales, pandas, databases |

Nested objects become dotted columns such as `amount.value`; lists stay as
compact JSON in one cell. Choose JSON for lossless processing.

## Interface

In a terminal, the export runs in a full-screen dashboard: sign-in steps and
accounts on the left, datasets with live progress on the right, a scrollable
log at the bottom. It adapts to light and dark terminals and to narrow windows.

| Key | Action |
|-----|--------|
| `enter` | Confirm the two-factor code |
| `ctrl+s` | Receive the code by SMS instead |
| `esc` | Cancel the prompt and the run |
| `↑` `↓` | Scroll the log |
| `o` | Open the output folder once done |
| `q` | Quit, canceling a run in progress |

When the dashboard closes, the list of written files stays on screen. Piped or
redirected output, `--plain` and `interface.mode = plain` switch to
line-by-line output instead, which suits logs and cron jobs; `NO_COLOR` is
honored.

## How it works

1. Headless Chrome loads the web app and yields an `aws-waf-token`, without
   which the API answers 403.
2. `POST /api/v1/auth/web/login` with the phone number and PIN starts a login
   process; the two-factor code completes it and returns a session cookie.
3. A WebSocket connection to `api.traderepublic.com` carries the data: each
   request is a `sub` frame answered by an `A` frame, or an `E` frame on
   rejection.
4. Accounts are discovered first, then every per-account dataset is fetched
   for each of them.

Credentials go to Trade Republic only: there is no server, no telemetry, no
third party. See [SECURITY.md](SECURITY.md).

## Upgrading from 0.1

The first run imports a `config.ini` found in the working directory into the
new location and moves the PIN to the keychain. The old file is left in
place: delete it, it still holds the PIN in clear text.

## Development

```bash
make test     # go test ./...
make race     # with the race detector
make demo     # run the dashboard with sample data
make help     # every target
```

| Package | Responsibility |
|---------|----------------|
| `internal/cli` | Commands and flags (Cobra, styled by Fang) |
| `internal/tui` | Full-screen dashboard and settings screen (Bubble Tea) |
| `internal/ui` | Line-by-line output for pipes and logs |
| `internal/report` | Contract between the pipeline and both interfaces |
| `internal/app` | Pipeline: sign in, discover accounts, export the datasets |
| `internal/config` | Settings file, keychain, environment |
| `internal/trws` | WebSocket protocol and subscriptions |
| `internal/auth` | Web login and two-factor flow |
| `internal/waf` | AWS WAF challenge in headless Chrome |
| `internal/export` | Dataset catalog, CSV and JSON writers |

The protocol is not documented by Trade Republic. It was pieced together from
the open-source clients credited below, and payloads are parsed tolerantly so a
renamed field degrades instead of crashing. Contributions are welcome: see
[CONTRIBUTING.md](CONTRIBUTING.md) and the [changelog](CHANGELOG.md).

## Credits

- [BenjaminOddou/trade_republic_scraper](https://github.com/BenjaminOddou/trade_republic_scraper),
  the Python tool this one started as a rewrite of
- [pytr-org/pytr](https://github.com/pytr-org/pytr), the most complete map of
  the Trade Republic subscription types
- [we-promise/sure#3986](https://github.com/we-promise/sure/pull/3986), which
  documents multi-account discovery through `accountPairs`
- [Charm](https://charm.land) for Bubble Tea, Lip Gloss and Fang

## License

MIT, see [LICENSE](LICENSE).

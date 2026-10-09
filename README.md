# trade-republic-exporter

Export your Trade Republic data to CSV or JSON from the command line: every
account (securities account, French PEA, and whatever product type Trade
Republic reports next), the holdings in each one including crypto, cash
balances, transactions, the activity log, savings plans and open orders.

Written in Go, no runtime dependency other than Chrome for the login
challenge, and a single static binary once built.

> [!WARNING]
> Unofficial tool. It drives the private API of the Trade Republic web app,
> which may break at any time and may conflict with the Trade Republic terms of
> service. Use it on your own account, at your own risk. Never commit
> `config.ini`.

```
  trade-republic-exporter dev

  Authentication
  ✓ Device identity          generated, set device_info to reuse it
  ✓ AWS WAF token            solved in headless Chrome (6.1s)
  ✓ Login                    +33*******78 accepted
  ? Two-factor code, 60s left (or type sms): 123456
  ✓ Two-factor code          session established

  Session
  ✓ WebSocket                connected to api.traderepublic.com
  ✓ Accounts                 2 found
    Securities account  1234567890  9876543210  EUR
    PEA                 2234567891  9876543211  EUR

  Export
  ✓ Accounts                 2 rows
  ✓ Positions                37 rows (2.4s)
  ✓ Cash balances            2 rows
  ✓ Available cash           2 rows
  ✓ Transactions             412 rows (9.7s)
  ✓ Activity log             388 rows (8.2s)
  ○ Savings plans            none reported
  ✓ Open orders              1 rows

  Files
    Accounts        2 rows    out/accounts.csv
    Positions       37 rows   out/positions.csv
    Cash balances   2 rows    out/cash.csv
    Available cash  2 rows    out/available_cash.csv
    Transactions    412 rows  out/transactions.csv
    Activity log    388 rows  out/activity_log.csv
    Open orders     1 rows    out/orders.csv

  Done
    7 files, 844 rows, 31.2s
```

## What gets exported

| File | Content | Subscription |
|------|---------|--------------|
| `accounts` | One row per account pair, with its product type, securities and cash account numbers | `accountPairs` |
| `positions` | Holdings of every securities account, one row per position, tagged with the owning account. Crypto is one of the categories Trade Republic returns | `compactPortfolioByType` |
| `cash` | Cash balance of each account | `cash` |
| `available_cash` | Cash available for trading, which excludes funds reserved by open orders | `availableCash` |
| `transactions` | Full transaction timeline, paged until exhausted | `timelineTransactions` |
| `activity_log` | Non-transaction events: logins, documents, account changes | `timelineActivityLog` |
| `orders` | Open orders | `orders` |
| `savings_plans` | Recurring investment plans | `savingsPlans` |

With `-details`, every transaction is enriched with its detail view (fees,
quantities, execution venue), at the cost of one extra request per transaction.

A subscription the API rejects is reported as a failed file and the run
continues with the others, with a non-zero exit code at the end. An error is
never written out as an empty result, so a rejected portfolio request cannot be
mistaken for an empty portfolio.

## Requirements

- Go 1.25 or newer, to build
- Chrome or Chromium, to solve the AWS WAF challenge that guards the API

## Install

```bash
go install github.com/souProjet/trade-republic-exporter@latest
```

Or build from a clone:

```bash
git clone https://github.com/souProjet/trade-republic-exporter.git
cd trade-republic-exporter
go build -o tr-export .
```

## Configure

```bash
cp config.example.ini config.ini
chmod 600 config.ini
```

| Key | Section | Description |
|-----|---------|-------------|
| `phone_number` | `secret` | Phone number in international format, for example `+33612345678` |
| `pin` | `secret` | Trade Republic PIN |
| `waf_token` | `secret` | Optional. Reuses a known WAF token instead of starting Chrome |
| `device_info` | `secret` | Optional. Reuses a device identity across runs, like the web client does |
| `output_format` | `general` | `csv` or `json` |
| `output_folder` | `general` | Destination directory, created if missing |
| `extract_details` | `general` | `true` to enrich every transaction |

Credentials can stay out of the file entirely, which is the better option on a
shared machine or in a scheduled job:

```bash
TR_PHONE_NUMBER=+33612345678 TR_PIN=1234 tr-export
```

`TR_WAF_TOKEN` and `TR_DEVICE_INFO` work the same way. The environment always
wins over the file.

## Use

```bash
tr-export                             # read config.ini, write CSV into out/
tr-export -format json -out exports   # JSON files into exports/
tr-export -details                    # enrich every transaction, slower
tr-export -quiet                      # only warnings and errors
tr-export -help
```

The two-factor code is requested on the terminal. Answer `sms` to receive it by
SMS instead of in the app.

Progress goes to stderr, so `-quiet` plus a redirect keeps automated runs
silent. Colors follow `NO_COLOR` and are disabled when the output is not a
terminal.

### CSV dialect

CSV output targets spreadsheet software in European locales: UTF-8 BOM,
semicolon separator, comma decimal separator, and `dd/mm/yyyy hh:mm` timestamps
in local time. Nested objects become dotted columns such as
`amount.value`; lists are kept as compact JSON in a single cell. Choose
`-format json` for machine processing.

## How it works

1. Headless Chrome loads the web app and yields an `aws-waf-token`, without
   which the API answers 403.
2. `POST /api/v1/auth/web/login` with the phone number and PIN starts a login
   process; the two-factor code completes it and returns a `tr_session` cookie.
3. A WebSocket connection to `api.traderepublic.com` carries the data. Each
   request is a `sub` frame answered by an `A` frame, an `E` frame on rejection,
   and the client unsubscribes as soon as the answer arrives.
4. Accounts are discovered first, then every per-account dataset is fetched for
   each of them.

Nothing is sent anywhere else: credentials go to Trade Republic only, and the
exported files are written with owner-only permissions.

## Development

```bash
go test ./...
go vet ./...
```

| Package | Responsibility |
|---------|----------------|
| `internal/waf` | Solves the AWS WAF challenge with headless Chrome |
| `internal/auth` | Web login, two-factor flow, session cookie |
| `internal/trws` | WebSocket protocol and subscriptions |
| `internal/export` | CSV and JSON writers, flattening, formatting |
| `internal/ui` | Terminal rendering: spinners, tables, prompts |
| `internal/app` | Pipeline: sign in, discover accounts, export the datasets |
| `internal/config` | INI file and environment configuration |

The protocol details are not documented by Trade Republic. They were pieced
together from the open-source clients credited below, and payload shapes are
parsed tolerantly so a renamed field degrades instead of crashing.

## Credits

- [BenjaminOddou/trade_republic_scraper](https://github.com/BenjaminOddou/trade_republic_scraper),
  the Python tool this one started as a rewrite of
- [pytr-org/pytr](https://github.com/pytr-org/pytr), the most complete map of
  the Trade Republic subscription types
- [we-promise/sure#3986](https://github.com/we-promise/sure/pull/3986), which
  documents multi-account discovery through `accountPairs`

## License

MIT, see [LICENSE](LICENSE).

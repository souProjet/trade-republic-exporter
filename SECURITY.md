# Security

## Reporting a vulnerability

Report anything that could expose credentials or exported data privately, not
in a public issue:

- GitHub: open a [private security advisory](https://github.com/souProjet/trade-republic-exporter/security/advisories/new)
- Email: swann.bougouin@splaze.fr

Please include the version, the platform and the steps to reproduce. Expect a
first answer within a few days. This is a side project, not a product with an
on-call rotation.

## Scope

This tool runs entirely on your machine. There is no server, no telemetry and
no third party: credentials go to `api.traderepublic.com` and nowhere else, and
headless Chrome only loads `app.traderepublic.com` to solve the AWS WAF
challenge.

What matters most on your side:

- The PIN is stored in the system keychain (macOS Keychain, Windows Credential
  Manager, Secret Service on Linux) under the service name
  `trade-republic-exporter`, never in the settings file. The command line
  refuses a PIN passed as an argument, which would stay in the shell history.
- Settings live in `~/.config/trade-republic-exporter/config.ini`, written
  with owner-only permissions (`0600`). It holds the phone number and the
  device identity, not the PIN.
- `TR_PIN` in the environment works for scheduled jobs; prefer your
  scheduler's secret store over a plain-text environment file.
- A v0.1 `config.ini` is imported on the first run and its PIN moved to the
  keychain, but the old file is left untouched: delete it.
- Exported files contain your full financial history. They are written with
  owner-only permissions (`0600`) in a directory created as `0700`.
- The session token lives in memory only and is never written to disk or
  logged. The interface redacts the phone number and never shows the WAF
  token, the session cookie or the PIN.

## Out of scope

Trade Republic owns the API, its availability and its terms of service. A
change on their side breaking this tool is a bug, not a vulnerability. Running
this tool may conflict with their terms of service; that risk is yours.

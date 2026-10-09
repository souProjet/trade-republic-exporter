# Contributing

Thanks for taking the time. Issues and pull requests are welcome, in English or
in French.

## Before you start

This tool talks to an undocumented API. Most bugs are not logic bugs but a
payload shape that changed on the Trade Republic side. When reporting one,
include the subscription name shown in the failing line and, if you can, the
payload with every personal value replaced.

Never paste credentials, a session cookie, a WAF token, an account number or an
unredacted export into an issue or a pull request.

## Development

```bash
go test ./...        # or: make test
make demo            # the dashboard with sample data, no account needed
go vet ./...
gofmt -l .           # must print nothing
make lint            # optional, needs golangci-lint
```

Everything runs offline: there is no test that calls the real API.

## Conventions

- US English for code, comments, documentation and commit messages.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):
  `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`. The release notes
  are generated from them.
- Keep the smallest change that solves the problem. No speculative abstraction.
- A new subscription belongs in `internal/trws`; a new exported file is an
  entry in `export.Datasets` plus a fetcher in `internal/app/datasets.go`; a
  new setting is an entry in `config.Keys`, which the editor, `config show`
  and completion all read from.
- The pipeline never prints: it reports through `report.Reporter`, which the
  dashboard and the line output both implement. Check a dashboard change with
  `make demo`, and keep the layout tests in `internal/tui` passing: they
  assert that every frame fits the terminal exactly.
- An API error must never be exported as an empty result. Return the error and
  let the pipeline report the dataset as failed.
- Add a test when the change is testable without the network: frame parsing,
  payload normalization, formatting, configuration.

## Pull requests

Keep one concern per pull request, make sure CI is green, and say in the
description what you ran against a live account, if anything. Reviews look at
correctness first, then at whether the change keeps the output stable for
people who already script around these files.

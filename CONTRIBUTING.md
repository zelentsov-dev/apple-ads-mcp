# Contributing

Use Go 1.26.6 or newer and keep the server compatible with the official MCP Go SDK v1.7 line.

Before opening a pull request:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
go run github.com/zricethezav/gitleaks/v8@v8.29.1 dir --no-banner --redact .
go run github.com/goreleaser/goreleaser/v2@v2.17.1 check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
npx --yes markdownlint-cli2@0.23.2 '**/*.md'
python3 scripts/validate_distribution.py
```

New Apple endpoints must have a specialized operation constructor, a bounded MCP schema, an operation classification, and HTTP tests. Do not add a raw request tool, configurable production base URL, secret logging, automatic mutation retry, deletion without inventory revalidation, or automatic budget increases.

Live checks are opt-in and read-only. Never place Apple credentials in fixtures, logs, issues, or pull requests.

# Contributing

Use Go 1.26.6 or newer and keep the server compatible with the official MCP Go SDK v1.7 line.

Before opening a pull request:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go run github.com/zricethezav/gitleaks/v8@v8.29.1 dir --no-banner --redact .
python3 scripts/validate_distribution.py
```

New Apple endpoints must have a specialized operation constructor, a bounded MCP schema, an operation classification, and HTTP tests. Do not add a raw request tool, configurable production base URL, secret logging, automatic mutation retry, deletion without inventory revalidation, or automatic budget increases.

Live checks are opt-in and read-only. Never place Apple credentials in fixtures, logs, issues, or pull requests.

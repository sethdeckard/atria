# Contributing to Atria

## Getting Started

```
git clone https://github.com/sethdeckard/atria.git
cd atria
make build
```

## Development

- **Build:** `make build`
- **Test:** `make test`
- **Lint:** `go vet ./...`

## Code Style

- Format with `gofmt` / `goimports`
- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- No `log.Fatal` in library code (`internal/` packages) — only `main.go` may exit the process

## Pull Requests

- Submit PRs against `main`
- Include a description of what changed and why
- Ensure `make test` passes

## Reporting Issues

Use [GitHub Issues](https://github.com/sethdeckard/atria/issues) to report bugs or request features.

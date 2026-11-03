# Repository Guidelines

## Writing standard

All prose MUST follow [ISO 24495-1:2023](https://www.iso.org/standard/78907.html). Write for the reader's task. Make information easy to find, understand, and use. Use clear sentences and preserve technical meaning.

## Project structure

Actionlint checks GitHub Actions workflows. The CLI lives in `cmd/actionlint/`. Public Go packages include `analysis/`, `analyzer/`, `source/`, and `diagnostic/`. Implementation code lives in `internal/`.

Tests sit beside Go source files. Fixtures live in `testdata/`, and CLI integration tests live in `internal/integration/cli/`. The `analyzers/` directory is a separate Go module. Documentation and assets live in `docs/` and `docs/assets/`. See `playground/` for the browser application and `examples/plugins/` for plugin examples.

## Build and development commands

Activate mise and run commands from the repository root. Tools are configured in `.mise.toml`; tasks are defined in `tasks.toml`.

- `runner install`: install tools and dependencies using frozen dependency settings.
- `go build ./cmd/actionlint`: build with checked-in generated sources.
- `run actionlint -h` (runs `go run ./cmd/actionlint -h`): display CLI help.
- `run generate`: regenerate sources; network access may be required.
- `run fmt`: format with `kprint fmt` / `kfmt`, which uses the owner's kprint fork of dprint.
- `run lint`: run architecture, Go, vulnerability, and platform-dependent WebAssembly checks.
- `run lint-docs`: validate documentation examples with ShellCheck.
- `run -s [TASK...]`: runs multiple tasks sequentially.
- `run -p [TASK...]`: runs multiple tasks in parallel.
- Use `runner --help`, and `run --help` for more options.
- Use `runner list` to see available tasks.
- Use `runner why [TASK]` to see what task runners will execute and why.
- Use `runner doctor` to check for common issues and misconfigurations.
- For dependencies we use `mise`. Run `mise which [BIN]` to see where a binary is sourced from. Run `mise exec -- command with --flags` to run a binary through mise's PATH.

## Coding style

Follow `dprint.toml`: gofumpt for Go, tabs and single quotes for TypeScript, and rustfmt for Rust. Biome handles linting. Use existing `package.json#imports` aliases. Public API signatures must not expose `internal` types.

## Testing

Use Go's `testing` package, `*_test.go` filenames, and `TestXxx` functions. Start with a focused regression:

```sh
go test ./internal/filefilter -run TestValidPatternPortablePaths
```

Run `run test` for the Go suite and `run test-analyzers` for the analyzer module. Windows tests omit `-race` by default. Applicable tests require Git, Bash, and ShellCheck.

Use `run test:js` for root JavaScript/TypeScript checks. The playground uses Node's test runner and Vitest through `run actionlint-playground:test`. Generate coverage with `run cov`. Codecov reports are informational and enforce no minimum.

## Commits and pull requests

Use concise subjects such as `docs: clarify setup` or `test(cli): cover invalid arguments`. Before committing, format, run `run go-modernize` in each affected Go module, and check the final staged content.

Explain motivation, scope, compatibility impact, and validation. Link relevant issues and authoritative sources. Follow `CONTRIBUTING.md`: preserve CLI contracts, document behavior changes, and keep policy checks opt-in.

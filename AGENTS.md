# Repository Guidelines

## Writing standard

All prose MUST follow [ISO 24495-1:2023](https://www.iso.org/standard/78907.html). Write for the reader's task. Make information easy to find, understand, and use. Use clear sentences and preserve technical meaning.

Before saving prose, check whether it tells the reader what happens or what to do. For instructions, state when to act and how to decide between any alternatives. Name the deciding condition instead of relying on words such as "appropriate" or "useful". Replace abstract phrases with the operation or behavior they describe, and remove sentences that add no information. Read each passage in context: do not replace words mechanically. When the intended behavior is unclear, inspect its implementation or specification before rewriting it; a wording change must not invent a new requirement.

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
- `run lint-docs`: run actionlint on documentation examples and compare the diagnostics with their documented output. ShellCheck analyzes shell scripts when available.
- `run -s [TASK...]`: runs multiple tasks sequentially.
- `run -p [TASK...]`: runs multiple tasks in parallel.
- Use `runner --help`, and `run --help` for more options.
- Use `runner list` to see available tasks.
- Use `runner why [TASK]` to see what task runners will execute and why.
- Use `runner doctor` to check for common issues and misconfigurations.
- For dependencies we use `mise`. Run `mise which [BIN]` to see where a binary is sourced from. Run `mise exec -- command with --flags` to run a binary through mise's PATH.

## Coding style

Follow `dprint.toml`: gofumpt for Go, tabs and single quotes for TypeScript, and rustfmt for Rust. Biome handles linting. Use existing `package.json#imports` aliases. Public API signatures must not expose `internal` types.

## Naming

Name an operation after what it does and a value after what it contains. A reader should understand a name at its call site without tracing the implementation or learning a project-specific umbrella term. Prefer resource-specific operations such as `get_repository_metadata`, `get_ref` and `get_object` to a generic name that hides which information is requested.

Read the implementation before choosing a replacement. Distinguish requests, responses, stored state, source locations and execution history by their actual roles. Rename an unclear abstraction rather than adding prose to explain a name that could state its purpose directly. Judge names in context; do not replace every occurrence of a word mechanically.

For wide Go symbol renames, use the Go language server (`gopls`) when available so references are updated semantically. Update wire fields, WIT interfaces, generated bindings, configuration, tests, examples and documentation as part of the same change. Regenerate files from their authoritative definitions.

Public API or wire-format compatibility must not block an authorized naming cleanup. Update affected consumers and validate the complete path. Do not add aliases whose only purpose is to preserve a rejected name.

## Testing

Use Go's `testing` package, `*_test.go` filenames, and `TestXxx` functions. Start with a focused regression:

```sh
go test ./internal/filefilter -run TestValidPatternPortablePaths
```

Run `run test` for the Go suite and `run test-analyzers` for the analyzer module. Windows tests omit `-race` by default. Applicable tests require Git, Bash, and ShellCheck.

Use `run test:js` for root JavaScript/TypeScript checks. The playground uses Node's test runner and Vitest through `run actionlint-playground:test`. Generate coverage with `run cov`. Codecov reports are informational and enforce no minimum.

## Defects found during development

Fix defects directly when feasible, addressing the shared cause and verifying affected callers. A defect too small to warrant an issue must be fixed rather than deferred.

If a substantial defect cannot be resolved directly, create a GitHub issue using `gh`, or add the evidence to an existing issue covering it. Include the affected revision, source location, minimal input, expected and actual behavior, and reproduction command. In the handoff, identify the fix and its verification or link the issue for work that is still open.

## Review artifacts

Reviewer instructions must require preservation of diagnostic programs, Go comparisons, probes, fixtures, reference outputs, and reproduction commands. Store exploratory artifacts under `.review-artifacts/` with their original language and enough context to reproduce the observation. Do not delete or replace an artifact merely because its finding was reported or a similar test exists.

After review, dispatch a sweeper agent to inventory these artifacts and promote their useful cases into permanent regression tests, fixtures, examples, or reference comparisons. Replace logging-only observations with assertions when promoting a regression. Record which permanent test or example covers each artifact and identify any coverage gaps. Preserve the original artifacts after promotion; remove them only when the user explicitly requests cleanup.

## Commits and pull requests

Use concise subjects such as `docs: clarify setup` or `test(cli): cover invalid arguments`. Before committing, format, run `run go-modernize` in each affected Go module, and check the final staged content.

Explain motivation, scope, compatibility impact, and validation. Link relevant issues and authoritative sources. Follow `CONTRIBUTING.md`: preserve CLI contracts, document behavior changes, and keep policy checks opt-in.

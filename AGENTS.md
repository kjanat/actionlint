# Repository instructions

## Writing standard

Follow the [writing instructions and examples](.agents/writing.md) for all prose.

## Project structure

`cmd/actionlint/` starts actionlint. `cmd/actionlint-lsp-wasm/` starts actionlint's language server in a browser worker. Both use the Go implementation in `internal/`. Write plugin code under `plugins/`.

| Directory                           | Contains                                         |
| ----------------------------------- | ------------------------------------------------ |
| `cmd/actionlint/`                   | The actionlint command                           |
| `cmd/actionlint-lsp-wasm/`          | actionlint's language server for browser workers |
| `internal/`                         | The Go implementation used by both programs      |
| `applications/`                     | The playground and GitHub Action                 |
| `libraries/`                        | The browser LSP client and changelog reader      |
| `plugins/protocol/`                 | Plugin interface definitions                     |
| `plugins/sdk/`, `plugins/examples/` | Plugin SDKs and examples                         |
| `distribution/`                     | Installation packages and build recipes          |
| `docs/`                             | Documentation and images                         |
| `internal/development/generators/`  | Go programs that generate source files           |
| `tools/`                            | Release and maintenance scripts                  |

Write unit tests in the package they test, with the files those tests need. Tests that exercise several packages together go in `internal/development/tests/integration/`. Tests that compare actionlint with the GitHub runner or Wasmtime go in `internal/development/tests/conformance/`.

## Build and development commands

Use `run` and `runner` for project tasks. They read `runner.toml` and the project's task definitions, including `tasks.toml`, to select the command for each task.

Run commands from the repository root. Prefix them with `mise exec --` to use the project's tools, for example `mise exec -- run fmt`. The task commands below omit this prefix.

| Command                                  | What it does                                                                                                               |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `runner install`                         | Installs tools and dependencies using the frozen settings in `runner.toml`.                                                |
| `mise exec -- go build ./cmd/actionlint` | Builds the CLI from source, using the generated files already in the repository.                                           |
| `run actionlint -h`                      | Runs actionlint from this checkout and displays help.                                                                      |
| `run generate`                           | Regenerates source files. Some generators need network access.                                                             |
| `run fmt`                                | Formats the repository with kprint.                                                                                        |
| `mise exec -- kprint fmt <paths>`        | Formats the specified files.                                                                                               |
| `run lint`                               | Runs Go checks, scans dependencies for known vulnerabilities, and checks the browser WebAssembly build.                    |
| `run test`                               | Runs the root Go module's tests.                                                                                           |
| `run test:js`                            | Runs the JavaScript/TypeScript checks.                                                                                     |
| `run lint-docs`                          | Runs the examples in `docs/checks.md` and compares their output with the documentation. Use this after editing an example. |

Use `run -s <task> <task>` to run tasks in sequence. For independent tasks, use `run -p <task> <task>` to run them in parallel.

These commands list tasks and help diagnose failures:

- Use `runner list` to list tasks.
- Use `runner why <task>` to see how a task would run, without executing it.
- Use `runner doctor` to investigate task discovery and tool configuration.
- Use `runner --help` or `run --help` for command options.

`mise which <binary>` prints the tool's executable path.

After editing a rule declaration, run `run -s generate:rules generate:config-schema`. This updates the rule documentation and configuration schema. If you edited only configuration types, use `run generate:config-schema`.

After editing a plugin interface, run `run -s protocol:sdk protocol:go contract` to update the SDK copies and regenerate the Go bindings. Test both the plugin and the actionlint code that loads or calls it. Run the Go SDK and Go example tests from their module directories because each has its own `go.mod`.

## Coding style

Check JavaScript/TypeScript edits with `mise exec -- biome check <paths>`. For imports within a package, use the aliases from the `imports` field in `package.json`.

## Naming

Function names must say what the function does and what data it handles. For example, `get_repository_metadata` fetches repository metadata. The variable `repository_metadata` holds the result.

Use `gopls` for Go renames if it is installed. Update every use of the name in code, plugin interfaces, SDKs, tests, examples and documentation. For JSON fields or WIT interfaces, update their consumers and regenerate the bindings too.

If the user asks you to rename something, replace the old name without adding an alias.

## Changing lint rules

Before adding a lint rule or editing an existing one, read the [write-rule skill](.agents/skills/write-rule/SKILL.md) and follow its instructions.

## Defects found during development

Fix small bugs during the task. If a bug cannot be fixed during the task, use `gh` to create or update an issue. Include:

- The commit, file and line.
- The smallest input needed to reproduce the bug.
- The expected and actual results.
- The reproduction command.

Link the issue in your final response.

## Files created during code review

Reviewers must save any programs they write, test inputs, reference-tool output and reproduction commands under `.review-artifacts/`. Save each program in the language it was written in.

After the review, ask another agent to turn the saved files into regression tests, comparisons with a reference tool or examples of supported behavior. Every test must compare actual and expected results.

Report where each saved program or test file was used. Explain why any were left unused. Delete the original review files only when the user asks you to.

## Commits and pull requests

Before committing:

1. Run `run fmt` to format the repository, or `mise exec -- kprint fmt <paths>` for specific files.
2. For changes in the root Go module, run `run go-modernize`. For each other modified Go module, run `mise exec -- modernize -fix ./...` from its directory. Both use the configured modernize wrapper.
3. Use `git diff` to check that the formatter and modernizer edits are correct and belong in this commit before staging them.
4. Test the changes. Rerun the tests after any further edits to the code under test.
5. Check `git diff --cached` before committing.

In the PR description, explain what you changed and why. List the checks you ran and link related issues. Explain any changes users must make to their configuration, commands or plugins. Update `docs/usage.md` when CLI behavior changes.

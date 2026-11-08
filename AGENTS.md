# Repository instructions

## Writing standard

All prose must follow [ISO 24495-1:2023](https://www.iso.org/standard/78907.html). Give clear directions, including when to follow them. If there are several options, explain when to use each. Read the code or specification before changing an explanation of how the software works.

## Project structure

The CLI and browser language server have their entry points in `cmd/`. Both use the Go implementation in `internal/`. Write plugin code under `plugins/`.

| Directory                           | Contains                                    |
| ----------------------------------- | ------------------------------------------- |
| `cmd/actionlint/`                   | The CLI program                             |
| `cmd/actionlint-lsp-wasm/`          | The browser language-server program         |
| `internal/`                         | The Go implementation used by both programs |
| `applications/`                     | The playground and GitHub Action            |
| `libraries/`                        | The browser LSP client and changelog reader |
| `plugins/protocol/`                 | Plugin interface definitions                |
| `plugins/sdk/`, `plugins/examples/` | Plugin SDKs and examples                    |
| `distribution/`                     | Installation packages and build recipes     |
| `docs/`                             | Documentation and images                    |
| `internal/development/generators/`  | Go programs that generate source files      |
| `tools/`                            | Release and maintenance scripts             |

Store unit tests and test inputs beside the implementation. Integration tests across packages go in `internal/development/tests/integration/`. Use `internal/development/tests/conformance/` for comparisons with the GitHub runner or Wasmtime.

## Build and development commands

Use `run` and `runner` for project tasks. They use the settings in `runner.toml` and the project's task definitions, including `tasks.toml`, to decide what to run.

Run commands from the repository root. Use `mise exec --` to load the project's tools, for example `mise exec -- run fmt`. For readability, the task commands in the table omit this prefix.

| Command                                  | What it does                                                                                                               |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `runner install`                         | Installs tools and dependencies using the frozen settings in `runner.toml`.                                                |
| `mise exec -- go build ./cmd/actionlint` | Builds the CLI from source, using the generated files already in the repository.                                           |
| `run actionlint -h`                      | Displays help for this checkout's CLI.                                                                                     |
| `run generate`                           | Regenerates source files. Some generators need network access.                                                             |
| `run fmt`                                | Formats the repository.                                                                                                    |
| `mise exec -- kprint fmt <paths>`        | Formats the specified files.                                                                                               |
| `run lint`                               | Runs Go checks, scans dependencies for known vulnerabilities, and checks the browser WebAssembly build.                    |
| `run test`                               | Runs the root Go module's tests.                                                                                           |
| `run test:js`                            | Runs the JavaScript/TypeScript checks.                                                                                     |
| `run lint-docs`                          | Runs the examples in `docs/checks.md` and compares their output with the documentation. Use this after editing an example. |

Use `run -s <task> <task>` to run tasks in sequence. For independent tasks, use `run -p <task> <task>` to run them in parallel.

To find tasks or diagnose a problem:

- Use `runner list` to list tasks.
- Use `runner why <task>` to see how a task would run, without executing it.
- Use `runner doctor` to investigate task discovery and tool configuration.
- Use `runner --help` or `run --help` for command options.

Use `mise which <binary>` to find where a tool is installed.

After editing a rule declaration, run `run -s generate:rules generate:config-schema`. This updates the rule documentation and configuration schema. If you edited only configuration types, use `run generate:config-schema`.

After editing a plugin interface, run `run -s protocol:sdk protocol:go contract`. This updates the SDK copies and regenerates the Go bindings. Test the plugin changes and the actionlint code for loading or calling the plugin. Run Go SDK and example tests from their module directories; each has its own `go.mod`.

## Coding style

Check JavaScript/TypeScript edits with `mise exec -- biome check <paths>`. For imports within a package, use the aliases from the `imports` field in `package.json`.

## Naming

Function names must describe the action and the data involved. For example, `get_repository_metadata` fetches repository metadata; `repository_metadata` holds the result.

Use `gopls` for Go renames if it is installed. Apply a rename throughout the code, plugin interfaces, SDKs, tests, examples and documentation. When renaming JSON fields or WIT interfaces, update the programs using them and regenerate the bindings.

When renaming something at the user's request, do not add an alias for the old name.

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

When assigning a review, require reviewers to save their programs, input files, output from reference tools and reproduction commands under `.review-artifacts/`. Save the programs in their original language.

After the review, give the saved files to another agent. Ask it to add regression tests for bugs or tests against a reference tool. A program can also become an example of supported behavior. Every test must compare actual and expected results.

For each review program or input file, write down where you used it. If you did not use a file, explain why. Delete the original review files only when the user asks you to.

## Commits and pull requests

Before committing:

1. Run `run fmt` to format the repository, or `mise exec -- kprint fmt <paths>` for specific files.
2. For changes in the root Go module, run `run go-modernize`. For each other modified Go module, run `mise exec -- modernize -fix ./...` from its directory. Both use the configured modernize wrapper.
3. Use `git diff` to check that the formatter and modernizer edits are correct and belong in this commit before staging them.
4. Test the changes. Rerun the tests after any further edits to the code under test.
5. Check `git diff --cached` before committing.

In the PR description, explain what you changed and why. List the checks you ran and link related issues. Explain any changes users must make to their configuration, commands or plugins. Update `docs/usage.md` when CLI behavior changes.

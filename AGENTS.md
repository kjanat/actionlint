# Repository instructions

Follow the [writing instructions](.agents/writing.md) for all prose.

## Project structure

Write unit tests in the package they test. Tests that exercise several packages go in `internal/development/tests/integration/`. Tests that compare actionlint with the GitHub runner or Wasmtime go in `internal/development/tests/conformance/`.

## Commands

| Command                                  | What it does                                                                      |
| ---------------------------------------- | --------------------------------------------------------------------------------- |
| `mise exec -- go build ./cmd/actionlint` | Builds the CLI without regenerating source files.                                 |
| `run generate`                           | Regenerates source files. Some generators need network access.                    |
| `run fmt`                                | Formats the repository. `mise exec -- kprint fmt <paths>` formats only `<paths>`. |
| `mise run test`                          | Runs the root Go module's tests.                                                  |
| `run test:js`                            | Runs the JavaScript/TypeScript checks.                                            |
| `run lint-docs`                          | Checks the examples in `docs/checks.md`. Run it after editing an example.         |

`run -s <task> <task>` runs tasks in sequence.

After editing a rule declaration, run `run -s generate:rules generate:config-schema`. After editing only configuration types, run `run generate:config-schema`.

After editing a plugin interface, run `run -s protocol:sdk protocol:go contract`. `plugins/sdk/go` and `plugins/examples/insecure-download` are separate Go modules. Run `go test ./...` inside each.

## Code

Check JavaScript/TypeScript edits with `mise exec -- biome check <paths>`. Import within a package through the aliases in the `imports` field of `package.json`.

Before adding or editing a lint rule, follow the [write-rule skill](.agents/skills/write-rule/SKILL.md).

## Defects found during development

For a bug you cannot fix, create or update an issue with `gh`. Include the commit, file and line, the smallest reproducing input, the expected and actual results, and the reproduction command. Link the issue in your final response.

## Files created during code review

Reviewers save their programs, test inputs, reference-tool output and reproduction commands under `.review-artifacts/`. After the review, ask another agent to turn those files into tests that compare actual and expected results. Report where each file was used and why any was left unused. Delete the review files only when the user asks.

## Commits and pull requests

Before committing, run `run fmt`. For changes in the root Go module, run `run go-modernize`. For each other modified Go module, run `mise exec -- modernize -fix ./...` in its directory. Check that the formatter and modernizer edits belong in the commit.

Update `docs/usage.md` when CLI behavior changes.

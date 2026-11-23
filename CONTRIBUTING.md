# Policy for actionlint's features

Correctness checks report a workflow when GitHub rejects it, when it runs differently from its authors' intent, and when it references something that does not exist. These checks must work without a configuration file.

Policy checks enforce practices beyond GitHub's requirements, such as pinning actions to commit hashes. New policy checks default to off. Give each policy check its own rule setting and document it in [the configuration document]. Configuration settings must distinguish an omitted value from an explicit disabling value. Use `*bool` for boolean keys and nil for omitted list or object values.

[The upstream project] rejects checks that enforce conventions and checks that require user configuration. Propose those checks here.

## AI usage and policy

AI-generated contributions are welcome. Disclosure of your tools is optional. Cite sources for claims about upstream behavior or the origin of code.

[Comment Cop] reviews added comments and documentation for wording problems. Its suggestions are advisory. Correct valid findings and resolve false-positive threads without editing the text.

## Compatibility and versioning

The CLI keeps the previous root invocations, flag syntax, templates and exit statuses alongside the new commands. We fix checks even when the fix makes previously passing workflows fail CI.

Releases follow Semantic Versioning for the CLI and configuration format. An incompatible change requires a major version and migration guidance, even when it fixes a correctness problem. New lint rules go in minor releases. Fixes to existing checks go in patch releases.

Connect other programs through the CLI, language server or plugin protocols described in [integration interfaces](docs/api.md).

## Reporting an issue

https://github.com/kjanat/actionlint/issues/new

Include a workflow that reproduces the problem.

## Sending a patch

https://github.com/kjanat/actionlint/pulls

- If you added a feature, add tests and explain it in [the usage document].
- If you updated [the checks document], run [the maintenance script].

The PR must explain the motivation, scope, compatibility impact and validation. Link authoritative sources for the design or behavior, such as GitHub documentation, specifications or upstream source pinned to a revision. When a refactor must stay in one PR, explain why and suggest a reading order.

## Development

With mise activated in your shell, run `runner install` from the repository root to install tools and dependencies. Installs are frozen. Use `runner install --no-frozen` when you update dependencies. Without an activated mise, prefix commands with `mise exec --`.

| Command             | Runs                                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------- |
| `mise run build`    | Generators, then the Go binary. Set `SKIP_GO_GENERATE=1` to skip the generators, which use the network. |
| `mise run test`     | Go tests with race checks                                                                               |
| `run cov`           | Go tests, then `coverage.html` and coverage per function                                                |
| `run lint`          | Go lint, generated-file checks, vulnerability scan and WebAssembly lint                                 |
| `run lint-docs`     | Documentation examples; requires ShellCheck                                                             |
| `run test:js`       | JavaScript/TypeScript tests                                                                             |
| `run go-modernize`  | Go modernize fixes                                                                                      |
| `mise run generate` | Source generators                                                                                       |

After changing a tool version, run `mise lock <tool>` and commit `mise.lock` with any files under `.mise/locks/`. To update a floating request such as `latest`, run `mise lock --bump <tool>`.

Run `run conformance` when changing parsing or validation. [Upstream conformance tests] compare actionlint with GitHub's language services and runner, SchemaStore and YAML Test Suite.

Workflow integration fixtures live in `internal/development/tests/integration/workflow/testdata/`:

- `examples/` holds the workflow examples from ['Checks' document].
- `ok/` holds workflows that produce no findings.
- `err/` holds invalid workflows and their expected diagnostics.
- `projects/` holds repository fixtures for checks that involve several files.

### Selecting CI checks

CI runs only the checks for changed projects and the projects that depend on them. A new project without assigned CI jobs runs every check. Run `mise exec -- run //.github/ci:test` after changing the planner.

To skip optional PR checks, add trailers to the latest PR head commit after a blank line:

```text
Refine CLI output

CI-Skip: nix, conformance, expression-probe
```

`CI-Skip` accepts only those three groups and is ignored on pushes to `master`. `CI: full` runs every check.

## Packaging source builds

Stamp the version with `-ldflags "-X actionlint.kjanat.dev/v2/internal/integrations/buildinfo.ReleaseVersion=<package-version>"`. With a different module path, the binary silently reports a development version.

GitHub's generated source downloads omit `man/actionlint.1`. The release source tarball includes it. Generate it with `run man:roff`. Pandoc 3.7 lacks `--syntax-highlighting=none`, so override the Pandoc command:

```sh
PANDOC='pandoc --standalone --from=markdown-smart --no-highlight' run man:roff
```

Install `actionlint.schema.json` beside the `schemas/` directory. Its references resolve relative to that location. Run `go test ./...` from the source root with Git, Bash and ShellCheck on `PATH`.

### Nix development

The [flake] builds the current checkout. Nix includes only files known to Git.

```sh
nix build
nix run . -- --help
nix flake check
nix develop
```

Format the Nix files with `nix fmt`. Update the package set with `nix flake update nixpkgs`, then run the checks. When Go dependencies change, set `vendorHash` in [nix/package.nix] to `lib.fakeHash`, run `nix build`, and copy the hash from Nix's output.

## Fuzzing

The targets in [`internal/development/tests/integration/fuzz/`] use [Go's built-in fuzzing]. Name one target:

```sh
FUZZ_FUNC=FuzzParse mise run fuzz
```

`mise run fuzz` without `FUZZ_FUNC` lists the targets. Failing inputs land in `internal/development/tests/integration/fuzz/testdata/fuzz/<target>/`. Commit them to add them to the seed corpus.

## Docker base images

[`Dockerfile`] selects its base images with the `GOLANG_VER` and `ALPINE_VER` build arguments. To move the defaults:

1. For Go, run `node tools/toolchain/update-go-toolchain.mjs go<major>.<minor>.<patch>` with Node, `gh` and Nix available. The script updates `go.mod`, the Docker builder default and the Nix pin together.
2. Update the Alpine `ARG` from [alpine] by hand.
3. Verify with `droast distribution/docker/Dockerfile` and `docker build -f distribution/docker/Dockerfile -t actionlint .`.
4. Send the upgrade as its own pull request.

## Releases

To release v1.2.3:

1. Describe the release in [CHANGELOG.md] under `Unreleased` or a `v1.2.3` section, and write the release notes to `docs/releases/v1.2.3.md`. That file becomes the GitHub release body, GoReleaser's notes and the WinGet manifest notes.
2. Commit the release changes on `master` and push them to `origin`.
3. Run `node tools/release/prepare-version.mjs --check` to check the version, changelog and release notes.
4. Run `node tools/release/prepare-version.mjs --push 1.2.3`. The script updates `VERSION` and the changelog, runs the Nix checks, pushes the commit and dispatches [release preparation](.github/workflows/release-prepare.yml). Drop `--push` to leave the edits for review. [The script README] lists prerequisites and recovery commands. On Windows, run Nix through WSL.
5. Inspect the draft after preparation succeeds. Then run `node tools/release/release-candidate.mjs promote --version 1.2.3 --run RUN_ID` locally. Promotion verifies the draft assets, signs `v1.2.3` at the tested commit, pushes `master` and the tag, and publishes the draft. Promotion overwrites manual edits to the draft. The signing key stays local.
6. Verify the release assets, npm packages, distribution updates and floating action tags. Published tags and assets are immutable. A change requires a new version.
7. To change published release notes, edit `docs/releases/v1.2.3.md` on `master`. [The release notes workflow](.github/workflows/release-notes.yml) copies the file to the GitHub release.

Write short changelog entries. Put examples and explanations of changed behavior in the release notes. Include contributor mentions and issue/PR references in both.

Dispatch `pypi-release.yml` with a published `tag` to publish only that release's wheels.

> [!NOTE]
> A failed WinGet submission opens an issue in `kjanat/actionlint`. Check the issue, job log and existing WinGet PRs, and resolve the problem before rerunning. An open WinGet PR for `kjanat.actionlint` blocks new submissions. Keep the `actionlint-winget-attempt-<version>` tags.

Check the package license fields in the release SBOMs.

## Manual

[`man/actionlint.1.md`] is the single source for the roff manual and the HTML page. `run man` builds both.

## Generated sources

The weekly [`Upkeep`] workflow regenerates these files in one PR. Review the upstream changes there before merging.

| File                                                                   | Task                | Generator                                                                             |
| ---------------------------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------- |
| [`internal/model/actionmeta/availability.gen.go`][action-availability] | `generate:actions`  | [`generate-action-metadata`] from the runner's template schema                        |
| `internal/model/actionmeta/runtimes.gen.go`                            | `generate:actions`  | `action-metadata -runtimes` from the runner's manifest parsers                        |
| [`webhooks.gen.go`]                                                    | `generate:events`   | [`generate-webhook-events`] from pinned schema and documentation copies               |
| [`internal/model/contexts/availability.gen.go`][`availability.gen.go`] | `generate:contexts` | [generate-availability] from [GitHub's contexts documentation][the contexts document] |

<a id="about-checks-doc"></a>

## Checks document

The [`check-checks`] script updates the code blocks after `Output:` and the `Playground` links. [The script README][the readme of the script-2] describes the required document format.

['Checks' document]: docs/checks.md
[CHANGELOG.md]: ./CHANGELOG.md
[Comment Cop]: .github/actions/comment-cop/
[Go's built-in fuzzing]: https://go.dev/doc/security/fuzz/
[Upstream conformance tests]: docs/conformance.md
[`Dockerfile`]: ./distribution/docker/Dockerfile
[`Upkeep`]: .github/workflows/upkeep.yml
[action-availability]: ./internal/model/actionmeta/availability.gen.go
[`webhooks.gen.go`]: ./internal/model/contexts/events/webhooks.gen.go
[`availability.gen.go`]: ./internal/model/contexts/availability.gen.go
[`check-checks`]: ./internal/development/tests/integration/documentation
[`internal/development/tests/integration/fuzz/`]: internal/development/tests/integration/fuzz
[`generate-action-metadata`]: ./internal/development/generators/action-metadata/main.go
[`generate-webhook-events`]: ./internal/development/generators/event-triggers
[`man/actionlint.1.md`]: ./man/actionlint.1.md
[alpine]: https://hub.docker.com/_/alpine
[flake]: flake.nix
[generate-availability]: ./internal/development/generators/expression-contexts
[nix/package.nix]: distribution/nix/package.nix
[the checks document]: docs/checks.md
[the configuration document]: docs/config.md
[the contexts document]: https://github.com/github/docs/blob/main/content/actions/reference/workflows-and-actions/contexts.md#context-availability
[the maintenance script]: #about-checks-doc
[the readme of the script-2]: ./internal/development/tests/integration/documentation/README.md
[the script README]: ./tools/release/README.md
[the upstream project]: https://github.com/rhysd/actionlint
[the usage document]: docs/usage.md

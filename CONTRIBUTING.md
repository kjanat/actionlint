# Policy for actionlint's features

actionlint checks workflow correctness and can also enforce project policies.

**Correctness checks** report workflows that GitHub rejects, that run differently than their authors intended, or that refer to something that does not exist. Repository settings can tell a check about local resources, such as self-hosted runner labels and configuration variables. Correctness checks must also work in a repository without a configuration file. Users can filter findings with `-ignore` or settings in [the configuration].

**Policy checks** report workflows that GitHub accepts but that do not follow a chosen practice, such as pinning actions to commit hashes or setting `timeout-minutes` on every job. Most are opt-in. The cache safety checks are enabled by default. Configure rules under `lint.rules` in [the configuration].

Patches for both kinds are welcome. Give each policy check its own rule setting and document it in [the configuration document]. New policy checks must default to off. A patch that makes an existing correctness check depend on configuration for its current behaviour is not accepted, because a repository with no configuration file must keep getting the same results.

Configuration settings must distinguish an omitted value from an explicit disabling value. Use `*bool` for boolean keys and nil for omitted list or object values. This lets a repository override inherited settings. Checks can use the resolved configuration for the run.

This is where the fork differs from [the upstream project], which accepts neither checks that enforce conventions nor checks that require user configuration. A patch turned down upstream for that reason is worth proposing here.

## AI usage and policy

I use AI/LLMs to assist with investigation, implementation, documentation, review (mixed use of coderabbit and codex/codex security reviews), and testing, including building fuzzing and compatibility-testing tools. I am responsible for the changes I accept and the releases I publish.

AI-generated contributions are welcome. Mentioning the tools you used can help explain your approach, but disclosure is optional and is not a review requirement. I do not consistently record tool usage either; keeping that inventory should not become an administrative burden.

The quality standard is the same for every contribution: understand what you submit, keep changes focused, write clear code and useful explanations, and validate the intended behavior. You are responsible for your submission, regardless of which tools helped produce it.

The principle is to judge contributions on technical merit and use tools to help maintainers. [Linus Torvalds's message on AI-assisted maintenance] expresses that approach well. Nobody is required to use AI to contribute here.

Contributions must explain the intended behavior and the validation performed. Passing tests do not establish that every downstream use case works. Claims about upstream behavior or provenance need evidence.

[Comment Cop] automatically reviews added comments and documentation and may submit a review with style findings. It is intentionally sensitive, and its suggestions are advisory. Address valid findings; if a finding is a false positive, you are welcome to resolve the review thread without changing the text. Keep explanations that help readers understand the code. Comment Cop does not establish whether code was AI-generated or replace correctness review. Quality is required whether or not it flags anything.

## CLI compatibility

Keeping existing workflows and CI integrations working is a priority. The CLI redesign preserves legacy root invocations, flag syntax, templates, and exit statuses alongside any new commands. Compatibility checks compare old and new executables across workflow fixtures, with regression tests covering argument parsing, output, and error behavior. These checks reduce regression risk without guaranteeing every downstream use case.

This commitment covers documented command invocations, flags, templates, output contracts, and exit-status meanings.

## Lint behavior and configuration

GitHub Actions evolves, and investigation of its behavior can uncover mistakes or gaps in existing checks. New and corrected correctness rules may flag workflows that previously passed, causing CI to fail. Correctness takes priority over preserving a clean result for a workflow with a newly detected problem.

The [GitHub Changelog monitor] checks for new Actions announcements daily and opens issues for review, using the [monitor action] and [changelog-feed package]. These announcements inform follow-up work; a tracked announcement does not mean its behavior is already supported.

The set of findings can change without changing the CLI interface: a new finding can change a run's exit status without changing what that status means. Configuration keys, accepted values, and their documented meanings are a separate compatibility contract, shared by consumers of the configuration format.

## Release versioning

Releases follow strict Semantic Versioning for the supported CLI and configuration contracts. Incompatible changes require a major version bump and documented migration guidance.

Backward-compatible features, including new lint rules, belong in minor releases. Backward-compatible fixes to existing checks belong in patch releases. A correctness motivation does not exempt an incompatible contract change from a major version bump.

## Integration interfaces

The Go host implementation is private. Integrations use the CLI, language server or plugin protocols. Plugin-author bindings live in the guest SDKs. See [integration interfaces](docs/api.md) to choose an entry point.

## Reporting an issue

To report a bug, search for similar issues and then open a new issue on GitHub.

https://github.com/kjanat/actionlint/issues/new

Include a workflow that reproduces the problem. Reports with only a small snippet or no input may receive lower priority because they can take longer to investigate.

## Sending a patch

To contribute a patch, open a pull request on GitHub.

https://github.com/kjanat/actionlint/pulls

Before submitting your PR, check the following:

- Confirm the build, tests and lint checks pass on your branch. The following sections explain how to run them.
- If you added a feature, consider adding tests and explaining it in [the usage document].
- If you added a public API, consider adding tests and a doc comment.
- If you updated [the checks document], ensure to run [the maintenance script].

### Larger changes and refactors

Large pull requests and substantial refactors are welcome. A heads-up before starting is appreciated. Opening an issue to discuss the problem and proposed approach can help establish scope before you invest substantial effort, but neither prior approval nor a discussion issue is required.

Keep the change focused and make it practical to review. Split independently useful changes into separate PRs where possible. When a refactor needs to stay together, explain why and give reviewers a suggested reading order.

Before starting, check [open pull requests] and [active branches] for overlapping or conflicting work. If you find related work, coordinate with its author and explain how your proposal relates to it.

The PR must explain the motivation, scope, compatibility impact, and validation performed. It must also link to authoritative sources wherever relevant to the design or behavior, such as official GitHub Actions documentation, specifications, or the relevant upstream source code. Prefer links pinned to a revision when discussing implementation details. Make it possible for a reviewer to check the reasoning without reconstructing your research.

## Development

Root tasks are defined in `tasks.toml`. Run them with `mise run <task>` or `run <task>`. Shared task templates live in `.mise.toml`. Their `sources` and `outputs` settings determine when a task needs to run again.

CI pins mise and runs one setup per job. `MISE_ENABLE_TOOLS` selects that job's tools, and `MISE_LOCKED=1` makes installation use `mise.lock`. The cache includes installed tools and mise's metadata, with separate keys for each platform and tool selection.

After changing a tool version, run `mise lock <tool>` and commit `mise.lock` with any dependency files under `.mise/locks/`. The platform list in `.mise.toml` covers Linux, macOS and Windows on x64 and ARM64. To update a floating request such as `latest`, run `mise lock --bump <tool>`. The README maintenance workflow refreshes its two actionlint versions this way before generating the comparison.

### Selecting CI checks

The planner asks mise which workspace projects changed and which projects depend on them. Mise reads Cargo and Bun workspace manifests. The project declarations in `.mise.toml` add the Go modules, the standalone Rust test host, and dependencies that cross languages. The planner maps the selected projects to CI jobs. It does not maintain file-matching rules.

A change confined to the Rust test host selects plugin and conformance checks. Changes to the Go implementation, embedded files or checked-in Wasm fixtures select the Go checks and their consumers. New projects without an assigned set of CI jobs run every check.

Required workflows start on every PR. Jobs that are not selected are skipped. The unit-test matrix reports its required check names through `ubuntu-slim` jobs that skip setup and tests. PR comparisons start at the merge base. Push comparisons include all pushed commits. Manual and scheduled runs select every check. If Git history or dependency information is unavailable, or a commit trailer is invalid, the planner runs every check.

Lint, JavaScript tests, Go guest-module tests and native Rust checks run before the expensive builds and platform matrices. A failed selected check blocks those jobs. A skipped check does not. All six unit-test platforms run when selected.

Run `mise exec -- run //.github/ci:test` to test the planner and job dependencies.

To skip optional PR checks, add trailers to the **latest PR head commit** after a blank line:

```text
Refine CLI output

CI-Skip: nix, conformance, expression-probe
```

`CI-Skip` accepts only those three optional groups. It cannot disable required checks, and it is ignored on pushes to `master`. Use `CI: full` to run all checks in the five selection-enabled workflows regardless of changed paths or `CI-Skip`. Older commit trailers and the generated PR merge commit do not control selection. Each selection job publishes the chosen checks in its job summary.

### Project task defaults

With mise activated in your shell, run `runner install` from the repository root. It installs the declared tools (including ShellCheck), installs npm workspace dependencies from the lockfile, and downloads Go modules. Installs are frozen by default; use `runner install --no-frozen` when intentionally updating dependencies. Use `runner config init` only to create a configuration in a new project; this repository already provides `runner.toml`.

Task names are unique across sources:

| Command                              | Runs                                                   |
| ------------------------------------ | ------------------------------------------------------ |
| `runner build`                       | Go binary via mise, using checked-in generated sources |
| `runner test`                        | Go tests via mise, including its default race checks   |
| `runner lint`                        | Go lint, vulnerability scan, and WebAssembly lint      |
| `runner lint-docs`                   | Documentation examples; requires ShellCheck            |
| `runner lint:biome`                  | Biome                                                  |
| `runner actionlint-playground:lint`  | Playground ESLint/type checking                        |
| `runner test:js`                     | Root JavaScript/TypeScript test tasks                  |
| `runner actionlint-playground:build` | Playground Vite build                                  |
| `runner actionlint-playground:test`  | Playground JavaScript tests                            |

`runner why lint` shows the selected command without executing it. Use `mise run generate` to regenerate sources. When mise is not activated, use `mise exec -- runner lint` to expose its tools.

Python linting is not part of actionlint. Documentation examples require ShellCheck; CI runs both lint targets.

## Building

```sh
go build ./cmd/actionlint
./actionlint -h
```

or

```sh
make build
```

`make build` runs `go generate` when generated sources need rebuilding. Generation uses the network. To build using the checked-in generated sources, add `SKIP_GO_GENERATE=1` to `make` arguments. Plain `go build` and `go install` use those sources without running the generators.

```sh
make build SKIP_GO_GENERATE=1
```

actionlint does not use cgo. Set `CGO_ENABLED=0` to avoid libc linking problems; `make build` does this by default.

### Packaging source builds

Use the module path from `go.mod` when stamping the version: `-ldflags "-X actionlint.kjanat.dev/v2/internal/integrations/buildinfo.ReleaseVersion=<package-version>"`. A different module path silently leaves the binary reporting a development version and linking to development documentation.

The source tarball uploaded by the release workflow includes `man/actionlint.1`. GitHub's automatically generated source downloads do not. Generate just the installed manpage with:

```sh
make man/actionlint.1
```

For Pandoc 3.7, which lacks `--syntax-highlighting=none`, override the command using its equivalent older option:

```sh
make man/actionlint.1 PANDOC='pandoc --standalone --from=markdown-smart --no-highlight'
```

Shell completions come from the built binary's `-completion bash`, `-completion zsh`, and `-completion fish` commands. For local configuration validation, install `actionlint.schema.json` and the `schemas/` tree together, preserving their relative paths so references such as `schemas/shellcheck/0.11.0.schema.json` resolve.

Run `go test ./...` from the source root, including when only `cmd/actionlint` is built. That command directory has no tests; implementation tests live with their packages and CLI integration tests in `internal/development/tests/integration/cli`. Put Git, Bash, and ShellCheck on `PATH` for the tests. Ordinary builds use the checked-in generated sources; dependency fetching can happen before an offline build, as with Nix's `buildGoModule`.

### Nix development

The [flake] builds the current checkout and pins Nixpkgs through `flake.lock`. From the repository root:

```sh
nix build
nix run . -- --help
nix flake check
nix develop
```

The default package runs `go test ./...` with the external linters and completion shells available. The flake's integration check verifies the installed version, help, configuration generation, package files, and ShellCheck diagnostics. `nix develop` provides Go, Git, Make, Pandoc, the linters, Bash, Zsh, Fish, and `nixfmt`. It sets `GOTOOLCHAIN=local` so Go uses the compiler selected by Nix. Format the Nix files with `nix fmt`.

The bump script updates the version in `flake.nix` together with the other release references and runs `nix flake check --no-update-lock-file` before creating a commit or tag. CI checks the package on every supported platform. Publishing binaries and images also requires those checks, including a match between the Nix version and the release tag. Update the package set with `nix flake update nixpkgs`, then run the checks. When Go dependencies change, update `vendorHash` in [nix/package.nix]: temporarily set it to `lib.fakeHash`, run `nix build`, and replace it with the hash reported by Nix.

Nix includes only files known to Git when building a checkout. Add new source files to Git before testing the flake.

## Testing

[![CI](https://github.com/kjanat/actionlint/actions/workflows/ci.yml/badge.svg)][CI] [![Upkeep](https://github.com/kjanat/actionlint/actions/workflows/upkeep.yml/badge.svg)][Upkeep] [![Problem Matchers](https://github.com/kjanat/actionlint/actions/workflows/matcher.yml/badge.svg)][Problem-Matchers] [![Download script](https://github.com/kjanat/actionlint/actions/workflows/download.yml/badge.svg)][Download-script] [![Release](https://github.com/kjanat/actionlint/actions/workflows/release.yml/badge.svg)][Release] [![Codecov](https://codecov.io/gh/kjanat/actionlint/graph/badge.svg?token=CgcOo0m9oW)][Codecov]

Run the following command at the root of this repository.

```sh
go test ./...
```

or

```sh
make test
```

To measure code coverage:

```sh
# Generate coverage.html and print the code coverage per functions
make cov
# See the coverage report in a browser (on macOS)
open coverage.html
```

The automated tests are organized as follows:

- [Upstream conformance tests] compare actionlint with fixtures from GitHub's language services and runner, SchemaStore, and YAML Test Suite. Run `make conformance` when changing parsing or validation. Baselines record known differences and detect changes; a green job does not mean full runtime compatibility.
- Unit tests use `*_test.go` files beside the implementation. Each package owns its fixtures in a local `testdata/` directory.
- Workflow integration tests own `internal/development/tests/integration/workflow/testdata/`.
  - `examples/` contains workflow examples from ['Checks' document]. Each `*.out` file records the expected diagnostics for its corresponding YAML input.
  - `ok/` contains workflows expected to produce no findings.
  - `err/` contains invalid workflows and their expected diagnostics.
  - `projects/` contains repository fixtures for checks involving several files, such as reusable workflows, local actions and configuration.

## Linting

[golangci-lint] runs the Go linters, configured by [`.golangci.toml`]. Install the binary as described in [its documentation]. CI pins the version in [`ci.yml`].

```sh
golangci-lint run
```

`.golangci.toml` turns on staticcheck's doc comment checks. A non-`main` package needs a package comment starting with `Package <name>`. A doc comment on an exported symbol must start with that symbol's name, with an optional leading article for types. An exported symbol carrying no doc comment at all is accepted.

[govulncheck] is used for security checks.

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

[modernize] rewrites code to newer Go idioms. golangci-lint reports its findings and the [autofix workflow] applies the fixes on every pull request. The same fixes can be applied locally with the following command.

```sh
go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@latest -fix ./...
```

Run these lint checks with the other checks using the following command.

```sh
make lint
make lint-docs # Requires ShellCheck for the examples
```

## Fuzzing

The targets in [`internal/development/tests/integration/fuzz/`] use [Go's built-in fuzzing], so no external tool is needed. Their seed corpora run as ordinary tests, which means `go test ./...` already compiles and exercises them.

`go test -fuzz` fuzzes exactly one target at a time, so name the one you want:

```sh
go test -run '^$' -fuzz '^FuzzParse$' ./internal/development/tests/integration/fuzz
```

or

```sh
FUZZ_FUNC=FuzzParse mise run fuzz
```

Running `mise run fuzz` without `FUZZ_FUNC` lists the available targets. Inputs that trigger a failure are written to `internal/development/tests/integration/fuzz/testdata/fuzz/<target>/` and become part of the seed corpus once committed.

## Update the pinned Docker base images

[`Dockerfile`] selects its base images with the `GOLANG_VER` and `ALPINE_VER` build arguments. Both default to explicit version tags and both can be overridden:

```sh
docker build -f distribution/docker/Dockerfile --build-arg GOLANG_VER=1.27.3 --build-arg ALPINE_VER=3.24 -t actionlint .
```

To move the defaults to newer base images:

1. Pick the new tags from Docker Hub ([golang], [alpine]). `GOLANG_VER` tracks the preferred `toolchain` in `go.mod`, which CI reads through `go-version-file`. `ALPINE_VER` selects the final runtime image independently of the Go builder.
2. For Go, run `node tools/toolchain/update-go-toolchain.mjs go<major>.<minor>.<patch>` with Node, `gh`, and Nix available. It verifies matching Nix compilers, then updates `go.mod`, the Docker builder default, this example, and the Nix pin and lock together. If no matching Nix compiler is available, it leaves the files unchanged. Update the Alpine `ARG` separately. CI and release builds use the Dockerfile defaults unless explicitly overridden.
3. Verify with `droast distribution/docker/Dockerfile` and `docker build -f distribution/docker/Dockerfile -t actionlint .`.
4. Send the upgrade as its own pull request.

## Make a new release

For v2, [Beepboop](https://github.com/kjanat/beepboop) dispatches the release workflows in [kjanat/homebrew-tap], `kjanat/scoop-bucket` and `kjanat/actionlint-pre-commit`. Its GitHub App credentials live centrally; source CI holds no Homebrew or Scoop token. Each destination independently verifies the signed source tag, the attested `release-candidate.json`, and the prepared asset digests before publishing with its own `GITHUB_TOKEN`. Repeated deliveries preserve identical content; downgrades and changed bytes for an existing version fail. The former `kjanat/homebrew-actionlint` tap contains migration metadata and is not updated by releases.

The publication workflow uses `AUR_SSH_PRIVATE_KEY` for AUR updates. Preparation receives no distribution credentials. After validating the published candidate, the release workflow calls the npm workflow, which publishes the platform packages before the launcher package. Its `pypi` job calls the PyPI workflow, which publishes the release's wheels to PyPI as `actionlint-kjanat` through trusted publishing. Preparation builds those wheels with [`distribution/pypi`](distribution/pypi/README.md) and attaches them to the draft. For a release without wheels, the PyPI workflow builds them from the release archives. Dispatching `pypi-release.yml` with a published `tag` publishes only that release's wheels.

Beepboop waits for `Verify published candidate` before dispatching package manifests, and also waits for PyPI and container publication before publishing hook tags. Dispatch is not publication success: inspect the destination workflow and Beepboop's `/operations` status. See [the v2 migration guide](docs/migration-v2.md) for hook publication.

Release preparation runs GoReleaser with `--skip=publish`: draft mode alone would still push package-manager updates. The tested binary archives, JavaScript bundle, container image and generated package manifests become draft assets. Promotion publishes those bytes without rebuilding them. Use local snapshots for development; promotion requires a prepared release candidate.

WinGet offers this fork as `kjanat.actionlint` (microsoft/winget-pkgs#430563). After the latest stable release is published, the release workflow's `winget` job generates the three package files with [`distribution/winget`](distribution/winget/README.md) from the verified release checksums and the published release body, validates them against the WinGet manifest schemas, and opens a PR in `microsoft/winget-pkgs` with them. The draft holds no WinGet files. Microsoft must validate and merge that PR before WinGet offers the new version. Prereleases are not submitted.

Set the repository secret `WINGET_TOKEN` to a token that can write to [the WinGet fork][fork repository] and open PRs in `microsoft/winget-pkgs`. Only the submission step receives this token.

You do not need to manually sync the WinGet fork before releasing; the script handles new submission branches. Before submitting, it checks the changed files of every open WinGet PR, across contributors and branches. An existing PR for any version of `kjanat.actionlint` blocks a new submission. Identical files already published by Microsoft are skipped.

Submission runs are serialized. The fork also keeps a shared submission lock and a permanent `actionlint-winget-attempt-<version>` tag. Once an attempt is recorded, reruns cannot create another PR for that version, including after a timeout or branch deletion. A final check withdraws our PR if another submission appeared during creation. If creation or withdrawal cannot be confirmed, the shared lock stays in place and blocks later submissions until a maintainer checks the outcome. GoReleaser's own WinGet upload is disabled; only the verified-manifest job submits.

A blocked or failed submission opens an issue in `kjanat/actionlint`, with the reason and workflow link. Reruns reuse the issue, reopening it if needed. A separate reporting job also covers failures before the submission script starts, job timeouts and cancelled submission jobs. Issue reporting itself needs GitHub to be available.

For example, to release v1.2.3:

1. Describe the release in [CHANGELOG.md], either under the `Unreleased` heading or in a `v1.2.3` section written out in full, and write the release notes to `docs/releases/v1.2.3.md`. That file is the body of the GitHub release, GoReleaser's release notes and the WinGet manifest notes. Version preparation refuses to run when the changelog describes nothing or the file is missing.
2. Validate and commit the release changes on `master`, including the changelog, and push them to `origin`.
3. Run `node tools/release/prepare-version.mjs --check` to list every declared version reference and check it against the repository.
4. Run `node tools/release/prepare-version.mjs --push 1.2.3`. It updates version references, runs the Nix checks, commits and pushes the source changes, then dispatches [release preparation](.github/workflows/release-prepare.yml). Drop `--push` to leave the edits for review; `--commit` creates only the source commit. Neither creates a version tag. See [the script README] for prerequisites and recovery commands. On Windows, Nix can run through WSL.
5. Wait for preparation to succeed and inspect the draft. It tests the candidate on Linux, macOS and Windows, checks Nix, and validates the container. Then run `node tools/release/release-candidate.mjs promote --version 1.2.3 --run RUN_ID` locally. Promotion verifies the successful run and every draft asset against its immutable manifest artifact, signs `v1.2.3` at the tested commit, records its ancestry with an `ours` merge, atomically pushes `master` and the tag, and publishes the draft. The tag contains the complete source tree, root `action.mjs`, and `SHA256SUMS`; generated files never enter the `master` tree. Pre-commit hooks are published separately from `kjanat/actionlint-pre-commit`. [The CI release job] distributes the published artifacts. Eligible floating `v1` and `v1.2` tags advance after its checks pass. All three references support the Action. The signing key stays local.
6. Verify the release assets, npm packages, distribution updates, and floating action tags. The bump script has already moved `Unreleased` entries into the dated release section with its anchor and comparison link; no manual post-release changelog edit is needed. Published release tags and assets are immutable; changing those requires a new version.
7. To change the notes after publication, edit `docs/releases/v1.2.3.md` on `master`. [The release notes workflow](.github/workflows/release-notes.yml) copies every changed file to its GitHub release; a file for a version without a release waits for that release. Promotion publishes the draft with the file's content, so a draft edited by hand is overwritten. Keep the changelog concise and the notes rich: examples, explanations of changed behavior, practical benefits. Preserve contributor mentions and issue/PR references in both, and verify examples against the released binary.
8. The Pages workflow redeploys the playground on pushes to `master` and after a successful release, deriving its version from Git. Upkeep refreshes the measured README demo after the release workflow succeeds.

The `make CHANGELOG.md` target runs [changelog-from-release], which rewrites the whole file from the GitHub releases. It knows nothing about the `Unreleased` heading and drops it, and the release bodies carry a `## What's changed` line the sections do not, so it does not round-trip this file.

> [!NOTE]
> If WinGet submission fails, check the issue, job log and existing WinGet PRs. Resolve the reported problem before taking further action. Keep permanent attempt tags; a leftover submission lock requires maintainer inspection before it is removed.

### Release SBOMs

GoReleaser generates SPDX SBOMs for release archives with Syft. The `sboms[].env` setting `SYFT_GOLANG_SEARCH_REMOTE_LICENSES=true` enables dependency license lookup from Go module sources, including on runners without a populated local module cache. This step requires network access to those sources.

Check the generated package license fields when verifying release assets. License lookup improves coverage; it does not guarantee a license for every dependency. `NOASSERTION` means the scanner did not determine the field. Syft does not extract copyright text, and its Go SPDX download-location support is tracked in anchore/syft#2087. Preserve fields with unknown values. See kjanat/actionlint#167 and [GoReleaser's SBOM configuration].

## How to generate the manual

[`man/actionlint.1.md`] is the single source. [pandoc] renders it to the roff manual `man/actionlint.1` and to `man/actionlint.1.html` for the site, which [`man/manual.css`] styles.

```sh
make man
```

## How to develop playground

Visit [`applications/playground/README.md`].

## How to deploy playground

The [Pages workflow] deploys on pushes to `master` and after successful Release runs triggered by published releases within this repository. Release-triggered builds check out the current default branch. The source badge and documentation links identify the built revision; a separate latest-release badge links to the published release. The build job has read-only repository access and builds the bundle with `run actionlint-playground:build`, packages `applications/playground/dist` together with the manual, and uploads it through `actions/upload-pages-artifact`.

Only the separate deployment job receives Pages write and OIDC permissions.

Install the tools in `.mise.toml` with `mise install` and activate mise in your shell. This makes the configured runner's `run` command available. Then check a build locally:

```sh
run actionlint-playground:build
run playground:preview
```

## Maintain auto-generated sources

Generators live with their owning subsystems under `internal/`: configuration schemas in `config/generate`, rule documentation in `rulecatalog/generate`, runner action tables in `action/generate`, and expression context data in `expression/context/generate`. Release tools live in `tools/release`, installers in `distribution/install`, and conformance tools in `internal/development/tests/conformance`. CI invokes these same entry points.

### Action metadata availability

[`availability.gen.go`][action-availability] contains composite step keys, expression contexts, and special-function argument limits derived from the runner's `action_yaml.json` template schema. `go generate` runs [`generate-action-metadata`], resolves the latest commit that changed the schema on `actions/runner`'s default branch, and fetches that revision from jsDelivr. The generated file records the source URL and commit. No schema copy or manually maintained revision file is stored in this repository.

The weekly `Upkeep` job includes this file in its usual generation PR. Review schema changes there before merging; incompatible schema changes fail generation without replacing the existing output. `Generated content` checks that regeneration matches the checked-in file. Both workflows provide `GH_TOKEN` for the GitHub API; local generation also accepts `GITHUB_TOKEN`, or uses the public API unauthenticated when neither is set.

To refresh only these tables, run `go run ./internal/model/actions/generate`. Normal builds and lint runs use the checked-in Go data and do not fetch the schema. Workflow expression availability is maintained separately below.

### JavaScript action runtime lifecycle

`go run ./internal/model/actions/generate -runtimes` refreshes `internal/model/actions/runtimes.gen.go`. It reads the runner's current and legacy manifest parsers for accepted `runs.using` values, `src/Misc/externals.sh` for bundled Node executables, and `Constants.cs` for deprecation notices and removal dates. The action template schema only requires a nonempty string at `runs.using`; it cannot supply the accepted versions or their lifecycle.

The generator resolves the newest commit touching those inputs and downloads all of them from jsDelivr at that revision. The generated header links that revision; `runtimeSourcePaths` in the generator lists the files. Parser disagreement, missing packaging data, or an unrecognized migration declaration fails generation before replacing the output. Review upstream source changes when that happens. No upstream source files are vendored, and ordinary builds and lint runs do not fetch anything.

`go generate` and weekly `Upkeep` refresh the runtime tables independently of action manifests. Remote manifests come from the host's metadata provider; there is no generated popular-action catalogue. Input/output checks also run when a supplied manifest declares a deprecated runtime. The generator identifies removed runtimes from the runner's packaging sources and includes scheduled removal dates in diagnostics. This data describes the upstream runner. Individual installations can use different migration dates and runtimes through GitHub's server-side flags.

### Maintain `webhooks.gen.go`

[`webhooks.gen.go`] is a table all webhooks supported by GitHub Actions to trigger workflows. Note that not all webhooks are supported by GitHub Actions.

`go generate` runs the [`generate-webhook-events`] script to generate it.

It fetches [`events-that-trigger-workflows.md`], parses the markdown document, and extracts webhook names and their types. For more details, see [README.md at the script directory].

The [`Upkeep`] workflow updates `webhooks.gen.go` weekly in CI.

### Maintain `actionlint-matcher.json`

[`actionlint-matcher.json`] is a matcher configuration to extract error annotations from outputs of `actionlint` command. See [the document] for its usage.

The regular expression must match output containing ANSI color escape sequences, so it is complex. Do not edit the JSON file manually.

The [`generate-actionlint-matcher`] script generates it. See the script’s README.md for usage and test instructions.

### Maintain `availability.gen.go`

[`availability.gen.go`] is a table for conversion from workflow key (like `jobs.<job_id>.if`) to availability of contexts and special functions. GitHub Actions limits contexts and functions in certain places. For example:

- limited workflow keys can access `secrets` context
- `jobs.<job_id>.if` and `jobs.<job_id>.steps.if` can use `always()` function.

`availability.gen.go` is generated from [the contexts document] using [generate-availability] script. It is run through `go generate` in root `generate.go`. See [the readme of the script] for the usage of the script.

The [`Upkeep`] workflow updates `availability.gen.go` weekly in CI.

<a id="about-checks-doc"></a>

## How to write checks document

The ['Checks' document]['Checks' document-2] explains actionlint's checks.

The [`check-checks`] script updates the code blocks after `Output:` and the `Playground` links. Run it after changing the document.

See [the script README][the readme of the script-2] for usage and the document format it expects.

['Checks' document-2]: ./docs/checks.md
['Checks' document]: docs/checks.md
[CHANGELOG.md]: ./CHANGELOG.md
[CI]: https://github.com/kjanat/actionlint/actions/workflows/ci.yml
[Codecov]: https://codecov.io/gh/kjanat/actionlint
[Comment Cop]: .github/actions/comment-cop/
[Download-script]: https://github.com/kjanat/actionlint/actions/workflows/download.yml
[GitHub Changelog monitor]: .github/workflows/changelog-monitor.yml
[Go's built-in fuzzing]: https://go.dev/doc/security/fuzz/
[GoReleaser's SBOM configuration]: https://goreleaser.com/customization/sbom/
[Linus Torvalds's message on AI-assisted maintenance]: https://lore.kernel.org/linux-media/CAHk-=wi4zC+Ze8e+p3tMv8TtG_80KzsZ1syL9anBtmEh5Z40vg@mail.gmail.com/
[Pages workflow]: ./.github/workflows/pages.yml
[Problem-Matchers]: https://github.com/kjanat/actionlint/actions/workflows/matcher.yml
[README.md at the script directory]: ./internal/model/contexts/events/payload/generate/README.md
[Release]: https://github.com/kjanat/actionlint/actions/workflows/release.yml
[Upkeep]: https://github.com/kjanat/actionlint/actions/workflows/upkeep.yml
[Upstream conformance tests]: docs/conformance.md
[`.golangci.toml`]: ./.golangci.toml
[`Dockerfile`]: ./distribution/docker/Dockerfile
[`Upkeep`]: .github/workflows/upkeep.yml
[action-availability]: ./internal/model/actions/availability.gen.go
[`actionlint-matcher.json`]: .github/actionlint-matcher.json
[`webhooks.gen.go`]: ./internal/model/contexts/events/payload/webhooks.gen.go
[`availability.gen.go`]: ./internal/model/contexts/availability.gen.go
[`check-checks`]: ./internal/development/tests/integration/documentation
[`ci.yml`]: .github/workflows/ci.yml
[`events-that-trigger-workflows.md`]: https://raw.githubusercontent.com/github/docs/refs/heads/main/content/actions/reference/workflows-and-actions/events-that-trigger-workflows.md
[`internal/development/tests/integration/fuzz/`]: internal/development/tests/integration/fuzz
[`generate-action-metadata`]: ./internal/model/actions/generate/main.go
[`generate-actionlint-matcher`]: ./tools/matcher
[`generate-webhook-events`]: ./internal/model/contexts/events/payload/generate
[`man/actionlint.1.md`]: ./man/actionlint.1.md
[`man/manual.css`]: ./man/manual.css
[`applications/playground/README.md`]: ./applications/playground/README.md
[active branches]: https://github.com/kjanat/actionlint/branches
[alpine]: https://hub.docker.com/_/alpine
[autofix workflow]: .github/workflows/autofix.yml
[changelog-feed package]: libraries/github-changelog/README.md
[changelog-from-release]: https://github.com/rhysd/changelog-from-release
[flake]: flake.nix
[fork repository]: https://github.com/kjanat/winget-pkgs
[generate-availability]: ./internal/model/contexts/generate
[golang]: https://hub.docker.com/_/golang
[golangci-lint]: https://golangci-lint.run/
[govulncheck]: https://go.dev/doc/security/vuln/
[its documentation]: https://golangci-lint.run/docs/welcome/install/
[kjanat/homebrew-tap]: https://github.com/kjanat/homebrew-tap
[modernize]: https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/modernize
[monitor action]: .github/actions/changelog-monitor/action.yml
[nix/package.nix]: nix/package.nix
[open pull requests]: https://github.com/kjanat/actionlint/pulls
[pandoc]: https://pandoc.org/
[the CI release job]: .github/workflows/release.yml
[the checks document]: docs/checks.md
[the configuration document]: docs/config.md
[the configuration]: docs/config.md
[the contexts document]: https://github.com/github/docs/blob/main/content/actions/learn-github-actions/contexts.md#context-availability
[the document]: docs/usage.md#problem-matchers
[the maintenance script]: #about-checks-doc
[the readme of the script-2]: ./internal/development/tests/integration/documentation/README.md
[the readme of the script]: ./internal/model/contexts/generate/README.md
[the script README]: ./tools/release/README.md
[the upstream project]: https://github.com/rhysd/actionlint
[the usage document]: docs/usage.md

## Architecture checks

`mise run lint` reuses the custom golangci-lint runner, rebuilding it when its sources or build configuration change. The [structural analyzers](analyzers/README.md) check public API types and representation ownership directly from source; no package inventory is required. Run `mise run test-analyzers` when changing them. Product findings fail CI.

# Policy for actionlint's features

actionlint has two kinds of checks.

**Correctness checks** report a workflow that GitHub rejects, that runs differently than its author meant, or that
refers to something which does not exist. They always run. A configuration key may tell such a check what exists in
this project, as `self-hosted-runner.labels` and `config-variables` do, but the check still exists and still reports
the same thing in a repository with no configuration file. Errors from them are filtered with the `-ignore` option or
the `ignore` key in [the configuration](docs/config.md).

**Policy checks** report a workflow that GitHub runs happily but that breaks a convention the project chose for
itself, such as pinning every action to a commit hash or setting `timeout-minutes` on every job. They stay silent
until the `policy` mapping in [the configuration](docs/config.md) turns them on, so a repository that configures
nothing never sees them. Being opinionated is fine in a check that only runs for the projects which asked for it.

Patches for both kinds are welcome. A patch for a policy check needs its own key under `policy`, a default of off,
and a section in [the configuration document](docs/config.md). A patch that makes an existing correctness check
depend on configuration for its current behaviour is not accepted, because a repository with no configuration file
must keep getting the same results.

Every new key must tell "not set" apart from "set to off". A boolean key is therefore a `*bool`, and a key with a
list or an object value uses nil for "not set". actionlint reads a single configuration file today, but it is meant
to read a user-global one as well, and at that point a key which cannot express "not set" leaves a repository unable
to opt out of what the user-global file turned on.

The configuration is read once at the start of a run, so a check may rely on it being available.

This is where the fork differs from [the upstream project](https://github.com/rhysd/actionlint), which accepts
neither checks that enforce conventions nor checks that require user configuration. A patch turned down upstream for
that reason is worth proposing here.

## Reporting an issue

To report a bug, please submit a new ticket on GitHub. It's helpful to search similar tickets before making it.

https://github.com/kjanat/actionlint/issues/new

Providing a reproducible workflow content is much appreciated. If only a small snippet of workflow is provided or no
input is provided at all, such issue tickets may get lower priority because they are occasionally time consuming to
investigate.

## Sending a patch

Thank you for taking your time to improve this project. To send a patch, please submit a new pull request on GitHub.

https://github.com/kjanat/actionlint/pulls

Before submitting your PR, please ensure the following points:

- Confirm build/tests/lints passed on your branch. How to run them is described in the following sections.
- If you added a new feature, consider to add tests and explain it in [the usage document](docs/usage.md).
- If you added a new public API, consider to add tests and a doc comment for the API.
- If you updated [the checks document](docs/checks.md), ensure to run [the maintenance script](#about-checks-doc).

Comment Cop may leave automated style suggestions on added comments and documentation. It is intentionally sensitive,
and its suggestions are advisory. If a finding is a false positive, you are welcome to resolve the review thread
without changing the text. Keep explanations that help readers understand the code.

Special thanks to the native English speakers for proofreading the documentation and error messages, as the author is not
proficient in English.

## Development

`make` (3.81 or later) is useful to run each tasks and reduce redundant builds/tests.

## Building

```sh
go build ./cmd/actionlint
./actionlint -h
```

or

```sh
make build
```

`make build` runs `go generate` when generated sources need rebuilding. Generation uses the network.
To build using the checked-in generated sources, add `SKIP_GO_GENERATE=1` to `make` arguments.
Plain `go build` and `go install` use those sources without running the generators.

```sh
make build SKIP_GO_GENERATE=1
```

Since actionlint doesn't use any cgo features, setting `CGO_ENABLED=0` environment variable is recommended to avoid troubles
around linking libc. `make build` does this by default.

## Testing

[![CI](https://github.com/kjanat/actionlint/actions/workflows/ci.yml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/ci.yml)
[![Upkeep](https://github.com/kjanat/actionlint/actions/workflows/upkeep.yml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/upkeep.yml)
[![Problem Matchers](https://github.com/kjanat/actionlint/actions/workflows/matcher.yml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/matcher.yml)
[![Download script](https://github.com/kjanat/actionlint/actions/workflows/download.yml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/download.yml)
[![Release](https://github.com/kjanat/actionlint/actions/workflows/release.yml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/release.yml)
[![Codecov](https://codecov.io/gh/kjanat/actionlint/graph/badge.svg?token=CgcOo0m9oW)](https://codecov.io/gh/kjanat/actionlint)

Run the following command at the root of this repository.

```sh
go test ./...
```

or

```sh
make test
```

To measure the code coverage

```sh
# Generate coverage.html and print the code coverage per functions
make cov
# See the coverage report in a browser (on macOS)
open coverage.html
```

Automated tests are as follows.

- [Upstream conformance tests](docs/conformance.md) compare actionlint with fixtures from GitHub's language services and
  runner, SchemaStore, and YAML Test Suite. Run `make conformance` when changing parsing or validation. Known differences
  remain visible and are checked for changes; a green job does not mean full runtime compatibility.
- Unit tests are implemented in `*_test.go` files for testing the corresponding APIs. Test data for unit tests are put in
  `testdata/` directory.
- UI tests based on matching to error messages are implemented in `linter_test.go` and all test data are stored in `testdata/`
  directory.
  - `testdata/examples/` contains tests for all examples in ['Checks' document](docs/checks.md). `*.yaml` files are an input
    workflow and `*.out` files are expected error messages.
  - `testdata/ok/` contains 'OK' tests. All workflow files in this directory should cause no errors.
  - `testdata/err/` contains 'Error' tests. Each `*.yaml` files are workflow inputs and corresponding `*.out` files are expected
    error messages (one error per line).
  - `testdata/projects/` contains 'Project' tests. Each directories represent a single project (meaning a repository on GitHub).
    Corresponding `*.out` files are expected error messages. Empty `*.out` file means the test case should cause no errors.
    'Project' test is used for use cases where multiple files are related (reusable workflows, local actions, config files, ...).

## Linting

[golangci-lint](https://golangci-lint.run/) runs the Go linters, configured by [`.golangci.toml`](./.golangci.toml).
Install the binary as described in [its documentation](https://golangci-lint.run/docs/welcome/install/). CI pins the
version in [`ci.yml`](.github/workflows/ci.yml).

```sh
golangci-lint run
```

`.golangci.toml` turns on staticcheck's doc comment checks. A non-`main` package needs a package comment starting with
`Package <name>`. A doc comment on an exported symbol must start with that symbol's name, with an optional leading
article for types. An exported symbol carrying no doc comment at all is accepted.

[govulncheck](https://go.dev/doc/security/vuln/) is used for security checks.

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

[modernize](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/modernize) rewrites code to newer Go idioms.
golangci-lint reports its findings and the [autofix workflow](.github/workflows/autofix.yml) applies the fixes on every
pull request. The same fixes can be applied locally with the following command.

```sh
go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@latest -fix ./...
```

These lints can be run with other checks by the following command.

```sh
make lint
```

## Fuzzing

The targets in [`fuzz/`](./fuzz) use [Go's built-in fuzzing](https://go.dev/doc/security/fuzz/), so no external tool is
needed. Their seed corpora run as ordinary tests, which means `go test ./...` already compiles and exercises them.

`go test -fuzz` fuzzes exactly one target at a time, so name the one you want:

```sh
go test -run '^$' -fuzz '^FuzzParse$' ./fuzz
```

or

```sh
make fuzz FUZZ_FUNC=FuzzParse
```

Running `make fuzz` without `FUZZ_FUNC` fails with a list of the available targets. Inputs that trigger a failure are
written to `fuzz/testdata/fuzz/<target>/` and become part of the seed corpus once committed.

## Update the pinned Docker base images

[`Dockerfile`](./Dockerfile) selects its base images with the `GOLANG_VER` and `ALPINE_VER` build arguments. Both default
to explicit version tags and both can be overridden:

```sh
docker build --build-arg GOLANG_VER=1.27.0 --build-arg ALPINE_VER=3.24 -t actionlint .
```

To move the defaults to newer base images:

1. Pick the new tags from Docker Hub ([golang](https://hub.docker.com/_/golang), [alpine](https://hub.docker.com/_/alpine)).
   `GOLANG_VER` tracks the Go version used by CI (`GO` in [`ci.yml`](.github/workflows/ci.yml)). `ALPINE_VER` tracks the
   Alpine release that `golang:<GOLANG_VER>-alpine` is built on.
2. Update the `ARG` defaults in `Dockerfile` together with the `GOLANG_VER` build arguments in
   [`ci.yml`](.github/workflows/ci.yml) and [`release.yml`](.github/workflows/release.yml).
3. Verify with `droast Dockerfile` and `docker build -t actionlint .`.
4. Send the upgrade as its own pull request.

## Make a new release

Updating [kjanat/homebrew-tap](https://github.com/kjanat/homebrew-tap) needs a `HOMEBREW_TAP_TOKEN` secret on this
repository, because the built-in `GITHUB_TOKEN` cannot write to another one. It is a fine-grained personal access token
whose repository access is that repository alone, with `Contents: Read and write`. The former
`kjanat/homebrew-actionlint` tap contains migration metadata and is not updated by releases.

GoReleaser also uses `SCOOP_BUCKET_TOKEN`, `WINGET_TOKEN`, and `AUR_SSH_PRIVATE_KEY` for the other distribution updates.
The npm reusable workflow runs after the binaries job and publishes the platform packages before the launcher package.
WinGet submissions still require review in `microsoft/winget-pkgs`; a successful release does not mean the package is
already available through WinGet.

WinGet uploads are paused with `winget[].skip_upload: true` in `.goreleaser.yaml` while
microsoft/winget-pkgs#430563 awaits review. GoReleaser still generates the manifests in `dist/`.
After the initial package is merged, restore `skip_upload: auto` to resume version submissions on stable releases.

When releasing v1.2.3 as example:

1. Describe the release in [CHANGELOG.md](./CHANGELOG.md), either under the `Unreleased` heading or in a `v1.2.3`
   section written out in full. The workflow seeds the release notes from the `v1.2.3` section when it exists and the
   `Unreleased` entries otherwise. `bump-version` refuses to run when neither describes anything.
2. Validate and commit the release changes on `master`, including the changelog, and push them to `origin`.
3. Run `go run ./scripts/bump-version -check` to list every declared version reference and confirm the declaration is in
   sync with the repository
4. Run `go run ./scripts/bump-version -push 1.2.3`. It updates every version reference, verifies the result, then creates
   and pushes the bump commit and the `v1.2.3` tag. Drop `-push` to leave the changes in the working tree for review, or
   use `-commit` to create the commit and the tag without pushing. See
   [the script README](./scripts/bump-version/README.md) for the declared files and fields.
5. Wait until [the CI release job](.github/workflows/release.yml) completes successfully. It resolves the release notes
   from the changelog and refuses to go further when they are missing, builds the manual, publishes the release binaries
   and their build provenance, updates the distributions, and pushes the CLI and action images to GHCR and Docker Hub.
   The floating `v1` and `v1.2` action tags move to a separate commit that pins the action image digest; the release tag
   remains on the version-bump commit. Floating-tag commits do not use the GPG signing service.
6. Verify the release assets, npm packages, distribution updates, and floating action tags. The bump script has already
   moved `Unreleased` entries into the dated release section with its anchor and comparison link; no manual
   post-release changelog edit is needed. Published release tags and assets are immutable; changing those requires a new version.
7. After publication, expand the GitHub release notes with examples, explanations of changed behavior, and practical
   benefits for users. Keep the changelog concise. Preserve contributor mentions and issue/PR references in both, and
   verify examples against the released binary. The release title and notes remain editable.
8. The Pages workflow redeploys the playground on pushes to `master` and after a successful release, deriving its version
   from Git. Upkeep refreshes the measured README demo after the release workflow succeeds.

The `make CHANGELOG.md` target runs [changelog-from-release](https://github.com/rhysd/changelog-from-release), which
rewrites the whole file from the GitHub releases. It knows nothing about the `Unreleased` heading and drops it, and the
release bodies carry a `## What's changed` line the sections do not, so it does not round-trip this file.

> [!NOTE]
> If you see workflow failure at releasing a new winget package, check the [fork repository](https://github.com/kjanat/winget-pkgs)
> is up-to-date. If it is outdated, click 'Sync fork' button to update it to the latest. And re-run the failed job
> again.

## How to generate the manual

[`man/actionlint.1.md`](./man/actionlint.1.md) is the single source. [pandoc](https://pandoc.org/)
renders it to the roff manual `man/actionlint.1` and to `man/actionlint.1.html` for the site, which
[`man/manual.css`](./man/manual.css) styles.

```sh
make man
```

## How to develop playground

Visit [`playground/README.md`](./playground/README.md).

## How to deploy playground

The [Pages workflow](./.github/workflows/pages.yml) deploys on pushes to `master` and after successful Release runs.
Release-triggered builds check out that release's commit. It builds the bundle with
`make -C playground build`, packages `playground/dist` together with the manual, and uploads it through
`actions/upload-pages-artifact`.

To check a build locally before pushing:

```sh
make -C playground build
npm run preview
```

## Maintain auto-generated sources

Some files are generated by scripts in [`scripts/`](./scripts) directory. These files are kept up-to-date by CI workflows.

### Action metadata availability

[`action_metadata_availability.go`](./action_metadata_availability.go) contains composite step keys, expression contexts,
and special-function argument limits derived from the runner's `action_yaml.json` template schema.
`go generate` runs [`generate-action-metadata`](./scripts/generate-action-metadata/main.go), resolves the latest commit
that changed the schema on `actions/runner`'s default branch, and fetches that revision from jsDelivr. The generated
file records the source URL and commit. No schema copy or manually maintained revision file is stored in this repository.

The weekly `Upkeep` job includes this file in its usual generation PR. Review schema changes there before merging;
incompatible schema changes fail generation without replacing the existing output. `Generated content` checks that
regeneration matches the checked-in file. Both workflows provide `GH_TOKEN` for the GitHub API; local generation also
accepts `GITHUB_TOKEN`, or uses the public API unauthenticated when neither is set.

To refresh only these tables, run `go run ./scripts/generate-action-metadata`. Normal builds and lint runs use the
checked-in Go data and do not fetch the schema. Workflow expression availability is maintained separately below.

### JavaScript action runtime lifecycle

`go run ./scripts/generate-action-metadata -runtimes` refreshes `action_runtimes.go`. It reads the runner's current and
legacy manifest parsers for accepted `runs.using` values, `src/Misc/externals.sh` for bundled Node executables, and
`Constants.cs` for deprecation notices and removal dates. The action template schema only requires a nonempty string
at `runs.using`; it cannot supply the accepted versions or their lifecycle.

The generator resolves the newest commit touching those inputs and downloads all of them from jsDelivr at that
revision. The generated header links that revision; `runtimeSourcePaths` in the generator lists the files. Parser
disagreement, missing packaging data, or an unrecognized migration declaration fails generation before replacing the
output. Review upstream source changes when that happens.
No upstream source files are vendored, and ordinary builds and lint runs do not fetch anything.

`go generate` refreshes runtime data before popular-action metadata; weekly `Upkeep` includes both in its PR.
Popular actions retain their JavaScript runtime alongside input/output metadata. Input/output checks continue when
the runtime is deprecated. The generator identifies removed runtimes from the runner's packaging sources and includes
scheduled removal dates in diagnostics. This data describes the upstream runner. Individual installations can use
different migration dates and runtimes through GitHub's server-side flags.

### Maintain `popular_actions.go`

[`popular_actions.go`](./popular_actions.go) is a data set of metadata of popular actions hosted on GitHub. It is generated
automatically with `go generate`. The command runs [`generate-popular-actions`](./scripts/generate-popular-actions) script.

The script also can detect new major releases of popular actions on GitHub by giving `-d` flag.

The [`Upkeep`](.github/workflows/upkeep.yml) CI workflow weekly runs to detect new major releases and update
`popular_actions.go`, and opens a pull request with the result. Runs can be found [actions/workflows/upkeep.yml].

[actions/workflows/upkeep.yml]: https://github.com/kjanat/actionlint/actions/workflows/upkeep.yml

### Maintain `all_webhooks.go`

[`all_webhooks.go`](./all_webhooks.go) is a table all webhooks supported by GitHub Actions to trigger workflows. Note that
not all webhooks are supported by GitHub Actions.

It is generated automatically with `go generate` running [`generate-webhook-events`](./scripts/generate-webhook-events) script.

It fetches [`events-that-trigger-workflows.md`](https://raw.githubusercontent.com/github/docs/refs/heads/main/content/actions/reference/workflows-and-actions/events-that-trigger-workflows.md),
parses the markdown document, and extracts webhook names and their types. For more details, see
[README.md at the script directory](./scripts/generate-webhook-events/README.md).

Updating `all_webhooks.go` is run weekly on CI by the [`Upkeep`](.github/workflows/upkeep.yml) workflow.

### Maintain `actionlint-matcher.json`

[`actionlint-matcher.json`](.github/actionlint-matcher.json) is a matcher configuration to extract error annotations from outputs
of `actionlint` command. See [the document](docs/usage.md#problem-matchers) for its usage.

The regular expression is complicated because it can matches to outputs which contain ANSI color escape sequences. So the JSON
file is not modified manually.

It is generated by [`generate-actionlint-matcher`](./scripts/generate-actionlint-matcher) script. See the README.md file for the
usage of the script and how to run the tests for it.

### Maintain `availability.go`

[`availability.go`](./availability.go) is a table for conversion from workflow key (like `jobs.<job_id>.if`) to availability of
contexts and special functions. GitHub Actions limits contexts and functions in certain places. For example:

- limited workflow keys can access `secrets` context
- `jobs.<job_id>.if` and `jobs.<job_id>.steps.if` can use `always()` function.

`availability.go` is generated from [the contexts document](https://github.com/github/docs/blob/main/content/actions/learn-github-actions/contexts.md#context-availability)
using [generate-availability](./scripts/generate-availability) script. It is run through `go generate` in `rule_expression.go`.
See [the readme of the script](./scripts/generate-availability/README.md) for the usage of the script.

Update for `availability.go` is run weekly on CI by the [`Upkeep`](.github/workflows/upkeep.yml) workflow.

<a id="about-checks-doc"></a>

## How to write checks document

The ['Checks' document](./docs/checks.md) is a large document to explain all checks by actionlint.

This document is maintained with [`check-checks`](./scripts/check-checks) script. This script automatically updates
the code blocks after `Output:` and the `Playground` links. This script should be run after modifying the document.

Please see [the readme of the script](./scripts/check-checks/README.md) for the usage and knowing the details of the
document format that this script assumes.

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

`make build` generates some sources with `go generate`. When you want to avoid it, add `SKIP_GO_GENERATE=1` to `make` arguments.

```sh
make build SKIP_GO_GENERATE=1
```

Since actionlint doesn't use any cgo features, setting `CGO_ENABLED=0` environment variable is recommended to avoid troubles
around linking libc. `make build` does this by default.

## Testing

[![CI](https://github.com/kjanat/actionlint/actions/workflows/ci.yaml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/ci.yaml)
[![Generate](https://github.com/kjanat/actionlint/actions/workflows/generate.yaml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/generate.yaml)
[![Problem Matchers](https://github.com/kjanat/actionlint/actions/workflows/matcher.yaml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/matcher.yaml)
[![Download script](https://github.com/kjanat/actionlint/actions/workflows/download.yaml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/download.yaml)
[![Release](https://github.com/kjanat/actionlint/actions/workflows/release.yaml/badge.svg)](https://github.com/kjanat/actionlint/actions/workflows/release.yaml)
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
version in [`ci.yaml`](.github/workflows/ci.yaml).

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
   `GOLANG_VER` tracks the Go version used by CI (`GO` in [`ci.yaml`](.github/workflows/ci.yaml)). `ALPINE_VER` tracks the
   Alpine release that `golang:<GOLANG_VER>-alpine` is built on.
2. Update the `ARG` defaults in `Dockerfile` together with the `GOLANG_VER` build arguments in
   [`ci.yaml`](.github/workflows/ci.yaml) and [`release.yaml`](.github/workflows/release.yaml).
3. Verify with `droast Dockerfile` and `docker build -t actionlint .`.
4. Send the upgrade as its own pull request.

## Make a new release

Updating [the Homebrew tap](https://github.com/kjanat/homebrew-actionlint) needs a `HOMEBREW_TAP_TOKEN` secret on this
repository, because the built-in `GITHUB_TOKEN` cannot write to another one. It is a fine-grained personal access token
whose repository access is `kjanat/homebrew-actionlint` alone, with `Contents: Read and write` and nothing else. The
GoReleaser step fails without it.

When releasing v1.2.3 as example:

1. Ensure all changes were already pushed to remote by checking `git push origin main` outputs `Everything up-to-date`
2. Describe the release in [CHANGELOG.md](./CHANGELOG.md), either under the `Unreleased` heading or in a `v1.2.3`
   section written out in full. The release notes are the `v1.2.3` section when it exists and the `Unreleased` entries
   otherwise, and `bump-version` refuses to run when neither describes anything.
3. Run `go run ./scripts/bump-version -check` to list every declared version reference and confirm the declaration is in
   sync with the repository
4. Run `go run ./scripts/bump-version -push 1.2.3`. It updates every version reference, verifies the result, then creates
   and pushes the bump commit and the `v1.2.3` tag. Drop `-push` to leave the changes in the working tree for review, or
   use `-commit` to create the commit and the tag without pushing. See
   [the script README](./scripts/bump-version/README.md) for the declared files and fields.
5. Wait until [the CI release job](.github/workflows/release.yaml) completes successfully. It resolves the release notes
   from the changelog and refuses to go further when they are missing, builds the manual, publishes the release binaries
   and their build provenance, pushes the CLI and action images to ghcr.io, and moves the `v1` tag.
6. Record the release in `CHANGELOG.md` under its own `v1.2.3` heading if it was released from `Unreleased`, following
   the shape of the sections around it: the `<a id="v1.2.3"></a>` anchor, the heading linking to the release page, the
   entries, the `[Changes][v1.2.3]` trailer, and the link definition at the end of the file. `bump-version -check`
   verifies all four parts of every section.
7. The Pages workflow redeploys the playground on the next push to `main`

The `make CHANGELOG.md` target runs [changelog-from-release](https://github.com/rhysd/changelog-from-release), which
rewrites the whole file from the GitHub releases. It knows nothing about the `Unreleased` heading and drops it, and the
release bodies carry a `## What's changed` line the sections do not, so it does not round-trip this file.

> [!NOTE]
> If you see workflow failure at releasing a new winget package, check the [fork repository](https://github.com/rhysd/winget-pkgs)
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

The [Pages workflow](./.github/workflows/pages.yaml) deploys on every push to `main`. It builds the bundle with
`make -C playground build`, packages `playground/dist` together with the manual, and uploads it through
`actions/upload-pages-artifact`.

To check a build locally before pushing:

```sh
make -C playground build
npm run preview
```

## Maintain auto-generated sources

Some files are generated by scripts in [`scripts/`](./scripts) directory. These files are kept up-to-date by CI workflows.

### Maintain `popular_actions.go`

[`popular_actions.go`](./popular_actions.go) is a data set of metadata of popular actions hosted on GitHub. It is generated
automatically with `go generate`. The command runs [`generate-popular-actions`](./scripts/generate-popular-actions) script.

The script also can detect new major releases of popular actions on GitHub by giving `-d` flag.

The [`generate`](.github/workflows/generate.yaml) CI workflow weekly runs to detect new major releases and update
`popular_actions.go`. Runs can be found [actions/workflows/generate.yaml].

[actions/workflows/generate.yaml]: https://github.com/kjanat/actionlint/actions/workflows/generate.yaml

### Maintain `all_webhooks.go`

[`all_webhooks.go`](./all_webhooks.go) is a table all webhooks supported by GitHub Actions to trigger workflows. Note that
not all webhooks are supported by GitHub Actions.

It is generated automatically with `go generate` running [`generate-webhook-events`](./scripts/generate-webhook-events) script.

It fetches [`events-that-trigger-workflows.md`](https://raw.githubusercontent.com/github/docs/refs/heads/main/content/actions/reference/workflows-and-actions/events-that-trigger-workflows.md),
parses the markdown document, and extracts webhook names and their types. For more details, see
[README.md at the script directory](./scripts/generate-webhook-events/README.md).

Updating `all_webhooks.go` is run weekly on CI by [`generate`](.github/workflows/generate.yaml) workflow.

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

Update for `availability.go` is run weekly on CI by [`generate`](.github/workflows/generate.yaml) workflow.

<a id="about-checks-doc"></a>

## How to write checks document

The ['Checks' document](./docs/checks.md) is a large document to explain all checks by actionlint.

This document is maintained with [`check-checks`](./scripts/check-checks) script. This script automatically updates
the code blocks after `Output:` and the `Playground` links. This script should be run after modifying the document.

Please see [the readme of the script](./scripts/check-checks/README.md) for the usage and knowing the details of the
document format that this script assumes.

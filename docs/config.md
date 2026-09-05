# Configuration

This document describes how to configure [actionlint](..) behavior.

The configuration file is optional. Every correctness check runs without it, so actionlint works fine in a repository
that has no configuration file. The file is where a repository tells actionlint what exists in its own environment,
and where it turns on the opt-in [policy checks](#policy-checks).

## Configuration file

Configuration file `actionlint.yaml` or `actionlint.yml` can be put in `.github` directory.

Note: If you're using [Super-Linter][Super-Linter], the file should be placed in a different directory. Please check the project's document.

For completion, hover documentation, and validation in editors using YAML Language Server, add this line to your config:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/kjanat/actionlint/HEAD/actionlint.schema.json
---
```

The [JSON Schema](../actionlint.schema.json) includes this fork's settings and is generated from the Go configuration
types, YAML tags, and comments. Regenerate it with `go generate -run generate-config-schema` (or `go generate` for all
generated files), then run `dprint fmt actionlint.schema.json` to apply the repository's schema formatting. CI checks
that it stays up to date. Custom YAML types have explicit mappings in
[`scripts/generate-config-schema`](../scripts/generate-config-schema/main.go); nullable values and field constraints
use `jsonschema` struct tags.

The schema rejects unknown keys everywhere. Runtime parsing ignores unknown keys at the top level, inside
`self-hosted-runner`, and inside each `paths` entry. For example, `config-secret` is silently ignored. Both validators
reject unknown keys inside `policy` and `require-job-timeout`. Go regular expression and glob syntax require additional
validation by actionlint when it loads the configuration.

```yaml
# Configuration related to self-hosted runner.
self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels:
    - linux.2xlarge
    - windows-latest-xl
    - linux-multi-gpu

# Configuration variables in array of strings defined in your repository or organization.
config-variables:
  - DEFAULT_RUNNER
  - JOB_NAME
  - ENVIRONMENT_STAGE

# Secrets in array of strings defined in your repository or organization.
config-secrets:
  - DEPLOY_TOKEN
  - API_KEY

# Which repository "Workflow permissions" setting to assume for a workflow call whose caller
# declares no permissions at all.
assume-default-permissions: restricted

# Path-specific configurations.
paths:
  # Glob pattern relative to the repository root for matching files. The path separator is always '/'.
  # This example configures any YAML file under the '.github/workflows/' directory.
  .github/workflows/**/*.{yml,yaml}:
    # List of regular expressions to filter errors by the error messages.
    ignore:
      # Ignore the specific error from shellcheck
      - "shellcheck reported issue in this script: SC2086:.+"
  # This pattern only matches '.github/workflows/release.yaml' file.
  .github/workflows/release.yaml:
    ignore:
      # Ignore errors from the old runner check. This may be useful for (outdated) self-hosted runner environment.
      - 'the runner of ".+" action is too old to run on GitHub Actions'
```

- `self-hosted-runner`: Configuration for your self-hosted runner environment.
  - `labels`: Label names added to your self-hosted runners as list of pattern. Glob syntax supported by [`path.Match`][pat]
    is available.
- `config-variables`: [Configuration variables][vars]. When an array is set, actionlint will check `vars` properties strictly.
  An empty array means no variable is allowed. The default value `null` disables the check.
- `config-secrets`: [Secrets][secrets]. When an array is set, actionlint checks `secrets` properties against the list.
  Names are compared case-insensitively. An empty array means no secret is allowed. The default value `null` disables
  the check. The secrets GitHub always provides (`GITHUB_TOKEN`, `ACTIONS_STEP_DEBUG`, `ACTIONS_RUNNER_DEBUG`) are
  always allowed. Secrets declared in `on.workflow_call.secrets` are also always allowed since a caller passes them.
- `assume-default-permissions`: Which repository "Workflow permissions" setting actionlint assumes when checking the
  permissions a [reusable workflow call](checks.md#check-permissions-of-workflow-call) passes on. It only applies to a
  calling job that declares no `permissions:` and whose workflow declares none either. `restricted` assumes the setting
  that grants read access to `contents` and `packages` and nothing else. `permissive` assumes the setting that grants
  write access, which still leaves `id-token` at `none` because OIDC always needs an explicit `permissions:` entry.
  Leaving the key out is the same as `restricted`. The setting lives in Settings > Actions > General > Workflow
  permissions, and `gh api repos/{owner}/{repo}/actions/permissions/workflow --jq .default_workflow_permissions` prints
  `read` for `restricted` and `write` for `permissive`.
- `paths`: Configurations for specific file path patterns. This is a mapping from a glob pattern and the corresponding
  configuration.
  - `{glob}`: A file path glob pattern to apply the configuration. The path separator is always '/'. It is matched to the
    relative path from the repository root. For example `.github/workflows/**/*.yaml` matches all the workflow files (with
    `.yaml` file extension). For the glob syntax, please read the [doublestar][doublestar] library's documentation.
    - `ignore`: The configuration to ignore (filter) the errors by the error messages. This is an array of regular
      expressions. When one of the patterns matches the error message, the error will be ignored. It's similar to the
      `-ignore` command line option.

## Policy checks

The keys under `policy` turn on checks that enforce a convention the repository chose for itself. GitHub runs a
workflow that violates one of them without complaining, so none of these checks reports anything until this mapping
turns it on, and a repository with no configuration file never sees them. Errors from the checks in
[the checks document](checks.md) are a different thing: those report a workflow that is broken, and they always run.

Each check owns one key. The key name is also the name of the rule, so it is the name in the `[...]` suffix of the
error message and the value of `{{$err.Kind}}` in the `-format` option. Each one adds its own subsection here, in
alphabetical order by key.

Every key tells three states apart. Writing `false`, or an empty list for a key whose value is a list, turns the
check off. Leaving the key out, or writing `null`, says nothing either way, so an empty `policy` mapping switches
nothing off. That distinction is what lets a key which says nothing take its value from elsewhere once actionlint
reads a user-global configuration file as well as the repository's. Today it reads one file: `-config-file` if given,
otherwise the repository's.

### require-commit-hash

This check reports a `uses:` which names something that can move. An action and a reusable workflow must give a ref of
40 or 64 hexadecimal digits, so a tag or a branch name is reported. A `docker://` image must give a digest in the
`{image}@{algorithm}:{hex}` form, so an image with a tag or with no tag at all is reported. A local reference
(`./path` or `$/path`) carries no ref and a `uses:` built with `${{ }}` cannot be read, so the check passes over them.

```yaml
policy:
  require-commit-hash: true
```

### require-job-timeout

This check reports a job which sets no `timeout-minutes:`. Such a job is cancelled after GitHub's default of 360
minutes. A job which calls a reusable workflow with `uses:` cannot set the key, so the check passes over it.

```yaml
policy:
  require-job-timeout: true
```

The value can also be a mapping. Its `max-minutes` key is the largest allowed number of minutes, and it must be
greater than zero. With it, a job whose `timeout-minutes:` is larger than that number is reported as well. A value
written with `${{ }}` is not compared because actionlint cannot read it.

```yaml
policy:
  require-job-timeout:
    max-minutes: 60
```

### required-actions

This check reports a workflow which does not use an action this repository requires. An entry is written like a `uses:`
value and both of its halves are glob patterns. `actions/checkout` accepts any ref, `actions/checkout@v5` accepts that
ref only, and `actions/checkout@v4*` accepts `v4` and `v4.2.2`. `*` does not match `/`, so `github/codeql-action/*`
matches every action in that repository. The name is matched case insensitively and the ref is matched case sensitively.

One error per missing action is reported at the first job of the workflow. Only the steps written in the workflow file
are searched, so the steps of a composite action and of a called reusable workflow are not. A workflow whose every job
calls a reusable workflow runs no step of its own, so it is passed over. So is a workflow with a `uses:` built with
`${{ }}`, because the action it names is not known before the workflow runs.

```yaml
policy:
  required-actions:
    - actions/checkout
    - my-org/security-scan@v2*
```

## Generate the initial configuration

You don't need to write the first configuration file by your hand. `actionlint` command can generate a default configuration
with `-init-config` flag.

```sh
actionlint -init-config
vim .github/actionlint.yaml
```

---

[Checks](checks.md) | [Installation](install.md) | [Usage](usage.md) | [Go API](api.md) | [References](reference.md)

[Super-Linter]: https://github.com/super-linter/super-linter
[pat]: https://pkg.go.dev/path#Match
[vars]: https://docs.github.com/en/actions/learn-github-actions/variables
[secrets]: https://docs.github.com/en/actions/security-guides/using-secrets-in-github-actions
[doublestar]: https://github.com/bmatcuk/doublestar

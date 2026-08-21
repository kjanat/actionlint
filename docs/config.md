# Configuration

This document describes how to configure [actionlint](..) behavior.

The configuration file is optional. Every correctness check runs without it, so actionlint works fine in a repository
that has no configuration file. The file is where a repository tells actionlint what exists in its own environment,
and where it turns on the opt-in [policy checks](#policy-checks).

## Configuration file

Configuration file `actionlint.yaml` or `actionlint.yml` can be put in `.github` directory.

Note: If you're using [Super-Linter][Super-Linter], the file should be placed in a different directory. Please check the project's document.

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
error message and the value of `{{$err.Kind}}` in the `-format` option. No policy check exists yet. Each one adds its
own subsection here, in alphabetical order by key.

Every key tells three states apart. Writing `false`, or an empty list for a key whose value is a list, turns the
check off. Leaving the key out, or writing `null`, says nothing either way, so an empty `policy` mapping switches
nothing off. That distinction is what lets a key which says nothing take its value from elsewhere once actionlint
reads a user-global configuration file as well as the repository's. Today it reads one file: `-config-file` if given,
otherwise the repository's.

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
[doublestar]: https://github.com/bmatcuk/doublestar

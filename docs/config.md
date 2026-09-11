# Configuration

This document describes how to configure [actionlint](..) behavior.

The configuration file is optional. Every correctness check runs without it, so actionlint works fine in a repository
that has no configuration file. The file is where a repository tells actionlint what exists in its own environment,
and where it turns on the opt-in [policy checks](#policy-checks).

## Configuration file

Configuration file `actionlint.yaml` or `actionlint.yml` can be put in `.github` directory.

Note: If you're using [Super-Linter][Super-Linter], the file should be placed in a different directory. Please check the project's document.

`actionlint -init-config` includes the YAML Language Server schema directive automatically. For completion, hover
documentation, and validation in an existing config, add:

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

The `@kjanat/actionlint` npm package includes and exports the schema for its release. If the package is installed in
your project, a config at `.github/actionlint.yaml` can use the local schema:

```yaml
# yaml-language-server: $schema=../node_modules/@kjanat/actionlint/actionlint.schema.json
```

The published schema is also available through a CDN for editor configuration:

```yaml
# yaml-language-server: $schema=https://cdn.jsdelivr.net/npm/@kjanat/actionlint/actionlint.schema.json
```

This URL follows the latest npm release and requires a release containing the schema. See the
[npm package documentation](../distribution/npm/facade/README.md#configuration-schema) for other CDN URLs and version
selection.

The schema rejects unknown keys everywhere. Runtime parsing ignores unknown keys at the top level, inside
`self-hosted-runner`, and inside each `paths` entry. For example, `config-secret` is silently ignored. Both validators
reject unknown keys inside `policy`, `require-job-timeout`, and `require-permissions`. Go regular expression and glob syntax
require additional validation by actionlint when it loads the configuration.

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
  # This pattern only matches '.github/workflows/release.yml' file.
  .github/workflows/release.yml:
    ignore:
      # Ignore errors from the old runner check. This may be useful for (outdated) self-hosted runner environment.
      - 'the runtime or service used by ".+" action is retired on GitHub.com'
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

The keys under `policy` configure checks for cache safety and repository conventions. The three cache policies below
are **enabled by default**, including when no configuration file exists. The remaining policies are opt-in.
These checks can report workflows that GitHub accepts: accepting a write grant does not make it safe, and a disabled
cache operation can silently do nothing.

Each check owns one key. The key name is also the name of the rule, so it is the name in the `[...]` suffix of the
error message and the value of `{{$err.Kind}}` in the `-format` option. Each one adds its own subsection here, in
alphabetical order by key.

Writing `false`, or an empty list for a list-valued key, turns that check off. Leaving the key out or writing `null`
retains its default, so `policy: {}` does not disable cache policies. actionlint reads one configuration file:
`-config-file` if given, otherwise the repository's.

### cache-call-unrestricted

Enabled by default. Requires an explicit `cache-mode` at a reusable call site, or inherited from its workflow, on
the low-trust triggers listed under [cache-write-untrusted](#cache-write-untrusted). GitHub's default read-only mode
does not cap a callee that explicitly asks for write access. Set `cache-mode: read` or `cache-mode: none` to establish
that cap. This applies to local and remote reusable calls, even when a local callee currently uses only read access;
the caller's restriction should survive a later callee change. Remote workflows are not downloaded.

```yaml
policy:
  cache-call-unrestricted: false
```

An explicit write-capable mode satisfies the declaration check but is reported separately by
`cache-write-untrusted`. Invalid declarations produce syntax diagnostics without an additional policy finding.

### cache-operation

Enabled by default. Reports official cache actions whose operation is disabled by the job's effective explicit mode:

| Action                  | Modes reported       |
| ----------------------- | -------------------- |
| `actions/cache/save`    | `read`, `none`       |
| `actions/cache/restore` | `write-only`, `none` |
| `actions/cache`         | `none`               |

Job declarations override workflow declarations. With `read`, the combined action can still restore; with `write-only`,
it can still save, so neither produces a finding for the combined action. Omitted modes are not guessed from event
payloads. Wrappers, custom cache actions and package-manager caching options are not inspected by this check.
Only the three exact entry points above are recognized. Owner and repository names are case-insensitive, but
the `save` and `restore` subpaths retain their case. Similarly named repositories and other subpaths are excluded.
GitHub skips a forbidden operation without failing the job; this diagnostic helps catch ineffective steps.

```yaml
policy:
  cache-operation: false
```

### cache-write-untrusted

Enabled by default. Reports explicit `write` and `write-only` grants on low-trust triggers that can use caches under
the default branch. These grants override GitHub's read-only default. If untrusted code or input controls the saved
contents, a later privileged workflow can consume a poisoned cache.

The checked triggers are `branch_protection_rule`, `check_run`, `check_suite`, `deployment`, `deployment_status`,
`discussion`, `discussion_comment`, `fork`, `gollum`, `image_version`, `issue_comment`, `issues`, `label`, `milestone`,
`public`, `pull_request_target`, `status`, `watch`, and `workflow_run`. Deployment events are included because their
target can be the default branch. A workflow with multiple triggers is checked if any listed trigger is present.

This is a conservative declaration check. It does not prove that attacker-controlled code executes, interpret `if`
guards, inspect cache keys, or establish the trust of downloaded artifacts. For a reviewed exception, use an
[inline suppression](#inline-cache-policy-exceptions). Ordinary `pull_request`, review events and `merge_group` use
their own refs and are not classified as this default-branch cache risk. Trusted write-default events, such as `push`,
and a standalone `workflow_call` trigger do not produce this finding.

Set `read` or `none` on the affected job. An inherited workflow declaration is reported once, and is not reported
if every job overrides it with a safe mode. Omitting the mode on an ordinary job retains GitHub's safe trigger default;
reusable calls are covered separately by `cache-call-unrestricted`.

```yaml
policy:
  cache-write-untrusted: false
```

The event classification follows GitHub's [cache access defaults](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#cache-access-for-low-trust-workflow-triggers)
and [event ref definitions](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows).
Tests require an explicit trust classification for every event in the generated webhook inventory. An unclassified
event is conservatively treated as restricted; event syntax validation still runs separately.

### Inline cache policy exceptions

Prefer an exception next to the reviewed declaration over disabling a policy for the entire repository:

```yaml
on: pull_request_target
cache-mode: write # actionlint:ignore cache-write-untrusted -- jobs use reviewed default-branch code only
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Report metadata without running pull request code"
```

Alternatively, put the directive on its own line immediately before the reported declaration:

```yaml
jobs:
  report:
    # actionlint:ignore-next-line cache-call-unrestricted -- reviewed callee manages its own cache limit
    uses: example/repository/.github/workflows/report.yml@main
```

Both forms require an exact rule name and a nonempty reason after `--`. A comma-separated list selects multiple
cache rules. Only `cache-call-unrestricted`, `cache-operation`, and `cache-write-untrusted` can be suppressed this way.
A directive affects the reported line only, including multiple findings of the selected rule on that line; it does
not affect other rules or later lines. Duplicate selectors have no additional effect; empty selectors are invalid.
The first `--` surrounded by spaces starts the reason, which may itself contain `--` or directive-like text.

Only comments attached to a declaration are interpreted. A blank line, another comment or a document separator
between a preceding directive and its target detaches it; orphan comments at the end of a document are also inert.
For attached directives, unknown selectors, missing reasons, standalone `ignore` and trailing `ignore-next-line`
report `inline-suppression` errors.

Use trailing comments on the same physical line as the reported value, or standalone comments immediately before
that line. For aliases, put the exception at the anchor declaration when the diagnostic points there. Text inside
quoted YAML strings or `run: |` scripts is not an actionlint directive. General inline ignores for other rules are
not supported; existing CLI and path-based ignore patterns remain available.

Inline exceptions are applied before CLI and path-based ignore patterns. Those patterns can filter remaining cache
findings and `inline-suppression` errors. A valid exception remains valid when another ignore also covers its finding;
there is no unused-suppression diagnostic. As with workflow parsing, only the first YAML document is inspected.

These findings use normal diagnostic output and exit status 1. Suppression removes only the selected finding; it
does not change cache access in GitHub Actions.

### disallow-suppressions

Prevent inline exceptions from hiding cache policy findings:

```yaml
policy:
  disallow-suppressions: true
```

With `true` or `{}`, actionlint reports each prohibited directive as `disallow-suppressions` at the comment and
retains the original violation at its source location. Both `actionlint:ignore` and `actionlint:ignore-next-line`
are covered. An inline directive cannot exempt itself from this policy. Omission, `null`, or `false` permits
inline exceptions as described above.

To restrict only specific rules or choose which diagnostics appear:

```yaml
policy:
  disallow-suppressions:
    rules: [cache-call-unrestricted, cache-write-untrusted]
    report: both
```

| `report`         | Prohibited directive | Original violation |
| ---------------- | -------------------- | ------------------ |
| `both` (default) | Reported             | Retained           |
| `suppression`    | Reported             | Suppressed         |
| `violation`      | Not reported         | Retained           |

Omitted `rules` selects all supported inline rule IDs. Explicit lists must be nonempty and contain only
`cache-call-unrestricted`, `cache-operation`, or `cache-write-untrusted`; duplicate entries have no additional
effect. A directive with multiple selectors can still suppress rules outside the prohibited set. Unknown fields,
rule IDs, report values, and null mapping fields are configuration errors.

In `both` and `suppression` modes, a valid prohibited directive is reported even when it hides no finding, including
when the underlying rule is disabled. `violation` mode only retains actual findings; it does not enable disabled
rules or invent a finding for an unused directive. Malformed directives still produce `inline-suppression` errors
and suppress nothing. The comment attachment and physical-line scope described above remain unchanged.

These controls follow the distinction between preventing local exceptions ([Rust's `forbid`](https://doc.rust-lang.org/stable/rustc/lints/levels.html#forbid))
and configuring how the linter handles inline directives ([ESLint's linter options](https://eslint.org/docs/latest/use/configure/configuration-files#configure-linter-options)).
Unused-directive reporting is a separate concern; enabling this policy does not add an unused-suppression check.

CLI and configured path ignore patterns still run afterwards and can filter either diagnostic. This
setting governs inline comments; it does not override those explicit filters or changes to the configuration itself.
Remaining diagnostics use the usual output formats and exit status 1.

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

The value can also be a mapping with `min-minutes` and `max-minutes`. Either bound can be omitted, and both are inclusive.
Each configured bound must be finite and greater than zero. The minimum must not exceed the maximum; actionlint validates
that relationship when reading configuration because JSON Schema cannot compare these two property values.
A value written with `${{ }}` is not compared because actionlint cannot evaluate it statically.

```yaml
policy:
  require-job-timeout:
    min-minutes: 5
    max-minutes: 60
```

This configuration accepts literal timeouts from 5 through 60 minutes. `{min-minutes: 5}` requires at least 5 minutes
without an upper bound. `{}` requires the key without limiting its value.

### require-permissions

This check requires an explicit `permissions:` declaration. It is disabled by default because GitHub accepts workflows
that inherit the repository's token permissions.

```yaml
policy:
  require-permissions: true
```

`true`, `{}`, and `{scope: workflow}` require a workflow-level declaration. `permissions: {}` satisfies the policy and
provides an empty baseline; grant the scopes each job needs on that job. A workflow with permissions declared only on
its jobs still needs the workflow-level declaration under this policy.

For a declaration on every job, use job scope:

```yaml
policy:
  require-permissions: { scope: job }
```

In this mode, a workflow-level declaration does not satisfy the check. Jobs calling reusable workflows are included:
GitHub permits `permissions:` on those calls, and the called workflow cannot elevate the permissions it receives.
The existing [reusable workflow permission check](checks.md#check-permissions-of-workflow-call) checks known caller/callee grants.

Either mode accepts an empty mapping, named scopes, `read-all`, or `write-all`. This policy checks whether the declaration
exists; it does not determine least privilege or require an empty workflow baseline. `assume-default-permissions` does
not disable the policy or change its diagnostics. Set `false` to disable it, or omit the key or use `null` to leave it unset.

### required-actions

This check reports a workflow which does not use an action this repository requires. An entry is written like a `uses:`
value and both of its halves are glob patterns. `actions/checkout` accepts any ref, `actions/checkout@v5` accepts that
ref only, and `actions/checkout@v7*` accepts `v4` and `v4.2.2`. `*` does not match `/`, so `github/codeql-action/*`
matches every action in that repository. The name is matched case insensitively and the ref is matched case sensitively.

One error per missing action is reported at the first job that runs its own steps. Only the steps written in the workflow file
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

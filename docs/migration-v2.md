# Migrating to v2

The v2 release and its first pre-commit hook tag are pending. These instructions
describe the upcoming cutover; existing immutable v1 releases remain available.

## pre-commit repository

Change `repo:` from `https://github.com/kjanat/actionlint` to
`https://github.com/kjanat/actionlint-pre-commit`. Select a published v2 tag from
that repository; old tags do not transfer between repositories.

```yaml
repos:
  - repo: https://github.com/kjanat/actionlint-pre-commit
    rev: <published-v2-tag>
    hooks:
      - id: actionlint
```

The placeholder is not an existing release. Replace it before running pre-commit.
`pre-commit autoupdate` updates revisions within the configured repository; it
cannot perform the URL change.

| Previous hook           | v2 choice                                                                        |
| ----------------------- | -------------------------------------------------------------------------------- |
| `actionlint`            | `actionlint`, now using the exact binary wheel in a Python environment           |
| `actionlint-shellcheck` | `actionlint` with the ShellCheck Wasm plugin declared in project configuration   |
| `actionlint-bundled`    | `actionlint` with ShellCheck and Ruff Wasm plugins, or the published Docker hook |
| `actionlint-system`     | `actionlint-system` with your separately installed v2 binary                     |
| `actionlint-docker`     | `actionlint-docker` from the new repository                                      |

Remove Go `additional_dependencies` entries used to install native ShellCheck.
The new default hook does not build Go source or provision native script linters.
Its pinned wheel supplies the host; the project's [plugin configuration and
lockfile](plugins.md) supply analyzer versions and artifact digests. Keep the
lockfile in version control.

Use `actionlint config init` for a new configuration, or `actionlint config migrate`
to inspect an existing configuration and `actionlint config migrate --write` to
apply it. Both default plugins must have published registry versions before
initialization or migration can install them. Explicit plugin declarations and
an empty `plugins: []` selection remain under project control.

Then run `pre-commit install` and `pre-commit run actionlint --all-files`.
Hooks still check every repository workflow, including commits that only delete
an action, reusable workflow, or configuration. The native `actionlint pre-commit`
command remains available.

Users remaining on v1 should keep their existing exact revision at the old URL.
The host repository's v2 source no longer contains the hook manifest; updating
that old URL to a v2 source revision will not install a hook.

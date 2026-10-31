# Migrating to v2

The v2 release and its first pre-commit hook tag are pending. These instructions
describe the upcoming cutover.

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
initialization or migration can install them. Default declarations are inserted
only when the project omits `plugins`.

Then run `pre-commit install` and `pre-commit run actionlint --all-files`.
Hooks still check every repository workflow, including commits that only delete
an action, reusable workflow, or configuration.

For v1, use an exact revision at the old URL.
The host repository's v2 source no longer contains the hook manifest; updating
that old URL to a v2 source revision will not install a hook.

## External tool selection

Tools are selected by id: `--tool ID[=COMMAND]`, `--no-tool ID`,
`ACTIONLINT_TOOL_<ID>_BIN`, `_FLAGS` and `_ENV`, the Action input `disable-tools`
and `analysis.Options.Tools`. The two v1.17.0 options still work for one release
as hidden, deprecated aliases and print a warning. The v1.17.0 Action inputs are
accepted and ignored.

| v1.17.0 surface           | v2 replacement                                                         |
| ------------------------- | ---------------------------------------------------------------------- |
| `-shellcheck COMMAND`     | `--tool shellcheck=COMMAND`; the old option is a deprecated alias      |
| `-shellcheck=`            | `--no-tool shellcheck`; the old option is a deprecated alias           |
| `-pyflakes COMMAND`       | Deprecated: runs Ruff with `--select F`; the command is never executed |
| `-pyflakes=`              | `--no-tool ruff`; the old option is a deprecated alias                 |
| Action input `shellcheck` | `disable-tools: shellcheck`; the input is ignored                      |
| Action input `pyflakes`   | `disable-tools: ruff`; the input is ignored                            |

Unreleased v2 builds also had `--ruff`, `--no-ruff`, `--no-shellcheck`,
`ACTIONLINT_SHELLCHECK_*`, `ACTIONLINT_RUFF_*`, the Action input `ruff` and the
library fields `analysis.Options.ShellCheck` and `analysis.Options.Ruff`. They are
removed without aliases; the `ruff` input is ignored like the v1.17.0 inputs. Use
`analysis.Options{Tools: map[string]analysis.Tool{"shellcheck": tool, "ruff": tool}}`
in the library; `analysis.Tools()` lists the valid map keys.

The last `--tool`, `--no-tool` or `-shellcheck` for an id wins, so
`--no-tool shellcheck -shellcheck=COMMAND` runs `COMMAND`. A disabled tool also
turns off plugins that provide it: a locked plugin whose name or alias is the id is
not downloaded, and any other plugin whose metadata names the id as its engine or a
rule is not loaded. Unreleased v2 builds skipped plugins by a `languages` metadata
key instead.

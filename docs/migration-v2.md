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
Its pinned wheel supplies actionlint; the project's [plugin configuration](plugins.md)
selects exact analyzer versions. The registry supplies immutable artifact records and
checksums; direct artifact declarations carry their own digests. Commit the configuration.

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

## Rule IDs and categories

V2 reports the canonical rule IDs. The five renamed IDs and old-to-new category
mapping are listed in [configuration migration](config-migration.md#rule-id-migration).
Old IDs still work in configured and inline suppressions. Current settings use
the four semantic categories `correctness`, `suspicious`, `security` and
`conventions`. Category levels follow current rule membership; set a rule
explicitly when its category changed and its former selection is still wanted.

## External tool selection

Tools are selected by id: `--tool ID[=COMMAND]`, `--no-tool ID`,
`ACTIONLINT_TOOL_<ID>_BIN`, `_FLAGS` and `_ENV`, and the Action input `disable-tools`.
The v1.17.0 `-shellcheck` and `-pyflakes` options remain hidden, deprecated aliases
for one release and print a warning. The v1.17.0 Action inputs with those names
are accepted but ignored.

| v1.17.0 surface           | v2 replacement                                                         |
| ------------------------- | ---------------------------------------------------------------------- |
| `-shellcheck COMMAND`     | `--tool shellcheck=COMMAND`; the old option is a deprecated alias      |
| `-shellcheck=`            | `--no-tool shellcheck`; the old option is a deprecated alias           |
| `-pyflakes COMMAND`       | Deprecated: runs Ruff with `--select F`; the command is never executed |
| `-pyflakes=`              | `--no-tool ruff`; the old option is a deprecated alias                 |
| Action input `shellcheck` | `disable-tools: shellcheck`; the input is ignored                      |
| Action input `pyflakes`   | `disable-tools: ruff`; the input is ignored                            |

The Go host implementation is private. Programs that previously embedded it
should use the [CLI, LSP or plugin interfaces](api.md).

The last `--tool`, `--no-tool` or `-shellcheck` option for an ID takes precedence, so
`--no-tool shellcheck -shellcheck=COMMAND` runs `COMMAND`. A disabled tool also
turns off plugins that provide it: a selected plugin whose name or alias is the id is
not downloaded, and any other plugin whose metadata names the id as its engine or a
rule is not loaded. Unreleased v2 builds skipped plugins by a `languages` metadata
key instead.

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

Tools are selected by id. The per-tool command-line flags, environment variables,
Action inputs and library fields still work for one release as deprecated aliases;
each command-line or environment alias prints a warning.

| v1 or earlier v2 surface                         | v2 replacement                                                                        |
| ------------------------------------------------ | ------------------------------------------------------------------------------------- |
| `--shellcheck[=COMMAND]`, `--ruff[=COMMAND]`     | `--tool shellcheck[=COMMAND]`, `--tool ruff[=COMMAND]`                                |
| `--shellcheck=`, `--ruff=false`                  | `--no-tool shellcheck`, `--tool ruff=false`                                           |
| `--no-shellcheck`, `--no-ruff`                   | `--no-tool shellcheck`, `--no-tool ruff`                                              |
| `--pyflakes COMMAND`                             | Deprecated alias declared by the bundled Ruff tool; `--pyflakes=` disables Ruff       |
| `ACTIONLINT_SHELLCHECK_BIN`, `_FLAGS`, `_ENV`    | `ACTIONLINT_TOOL_SHELLCHECK_BIN`, `_FLAGS`, `_ENV`                                    |
| `ACTIONLINT_RUFF_BIN`, `_FLAGS`, `_ENV`          | `ACTIONLINT_TOOL_RUFF_BIN`, `_FLAGS`, `_ENV`                                          |
| Action inputs `shellcheck: false`, `ruff: false` | `disable-tools: shellcheck` or `disable-tools: ruff`                                  |
| Action input `pyflakes`                          | Ignored, as before                                                                    |
| `analysis.Options{ShellCheck: tool, Ruff: tool}` | `analysis.Options{Tools: map[string]analysis.Tool{"shellcheck": tool, "ruff": tool}}` |

The deprecated library fields `analysis.Options.ShellCheck` and
`analysis.Options.Ruff` are used only when `Options.Tools` has no key for that
tool. `analysis.Tools()` lists the valid map keys.

Two behaviors differ from the old flags. The last `--tool` or `--no-tool` for an id
wins, so `--no-ruff --ruff=COMMAND` now runs `COMMAND`. A disabled tool also turns
off plugins that provide it: a locked plugin whose name or alias is the id is not
downloaded, and any other plugin whose metadata names the id as its engine or a
rule is not loaded. The former `--no-ruff` and `--no-shellcheck` skipped plugins by
a `languages` metadata key instead.

# Migrating to v2

## pre-commit repository

Change `repo:` to `https://github.com/kjanat/actionlint-pre-commit` and select a
v2 tag. `pre-commit autoupdate` cannot change the repository URL.

```yaml
repos:
  - repo: https://github.com/kjanat/actionlint-pre-commit
    rev: <published-v2-tag>
    hooks:
      - id: actionlint
```

| Previous hook           | v2 hook                                                   |
| ----------------------- | --------------------------------------------------------- |
| `actionlint-shellcheck` | `actionlint` with the ShellCheck Wasm plugin              |
| `actionlint-system`     | `actionlint-system` with a separately installed v2 binary |

Run
`actionlint config migrate --write`, or `actionlint config init` for a new
project, and commit the result. Neither command adds the default plugins to an
existing `plugins` list. Then run `pre-commit install` and
`pre-commit run actionlint --all-files`. To stay on v1, keep
`https://github.com/kjanat/actionlint` and pin a v1 tag.

## Rule IDs and categories

[Configuration migration](config-migration.md#rule-id-migration) lists the
renamed rule IDs and categories. To keep the
former level of a rule that moved category, set the rule explicitly.

## External tool selection

| v1.17.0                   | v2                                                |
| ------------------------- | ------------------------------------------------- |
| `-shellcheck COMMAND`     | `--tool shellcheck=COMMAND`                       |
| `-shellcheck=`            | `--no-tool shellcheck`                            |
| `-pyflakes COMMAND`       | Runs Ruff with `--select F` and ignores `COMMAND` |
| `-pyflakes=`              | `--no-tool ruff`                                  |
| Action input `shellcheck` | `disable-tools: shellcheck`                       |
| Action input `pyflakes`   | `disable-tools: ruff`                             |

The `-shellcheck` and `-pyflakes` options are deprecated. The last option for a tool ID wins. A disabled tool also
disables the plugins that provide it.

Embed actionlint through the
[CLI, LSP or plugin interfaces](api.md).

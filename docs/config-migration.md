# Configuration migration

`actionlint config migrate --config .github/actionlint.yaml` previews the current syntax without writing. `--write` replaces the file, `--check` exits with status 1 when conversion is needed, and `--json` prints the converted content and notes.

When `plugins` is absent, migration adds the default ShellCheck and Ruff plugins. An explicit `plugins` value, even `[]` or `null`, prevents that.

`--write` resolves the plugins and saves their exact registry versions. Preview and `--check` download nothing.

actionlint still reads old documents. Before writing, actionlint fetches every inherited configuration again and stops if one has changed. TOML cannot express null resets or unknown variable values. Convert such a document to YAML or JSON first.

| Old setting                  | Current setting                                                                                       |
| ---------------------------- | ----------------------------------------------------------------------------------------------------- |
| `self-hosted-runner.labels`  | `repository.runners`                                                                                  |
| `config-variables`           | `repository.variables` (names become keys with null values)                                           |
| `config-secrets`             | `repository.secrets`                                                                                  |
| `assume-default-permissions` | `repository.default-workflow-permissions` (`restricted` becomes `read`; `permissive` becomes `write`) |
| `policy.<rule>`              | `lint.rules.<group>.<rule>`                                                                           |
| `paths.<glob>.ignore`        | `lint.message-ignores.<glob>`                                                                         |

<a id="rule-id-migration"></a>

Configuration and suppressions accept the old rule IDs as aliases, except the permission-rule names. Migration rewrites old IDs to the current IDs.

| Previous ID               | Current ID                |
| ------------------------- | ------------------------- |
| `require-commit-hash`     | `unpinned-reference`      |
| `require-job-timeout`     | `job-timeout`             |
| `require-permissions`     | `missing-permissions`     |
| `require-action-metadata` | `missing-action-metadata` |
| `disallow-suppressions`   | `forbidden-suppression`   |

`missing-permissions` also replaces `explicit-permissions` and `missing-workflow-permissions`. Migration converts `scope: workflow` to `require-at: workflow` and `scope: job` to `require-at: every-job`. A selected `missing-workflow-permissions` rule keeps its warning level. New configurations report missing declarations as errors. Inline suppressions must use `missing-permissions`.

The categories are `correctness`, `suspicious`, `security` and `conventions`. `style` maps to `conventions`, and each `policy` rule moves to its own category.

In `repository`, null means unknown and an empty collection means a known empty set.

```yaml
repository:
  runners: [gpu]
  variables: {}
  secrets: null
lint:
  rules:
    policy:
      require-job-timeout:
        level: on
        options: { min-minutes: 5, max-minutes: 30 }
  message-ignores:
    ".github/workflows/legacy.yml": ["legacy diagnostic text"]
```

actionlint rejects conflicting old and current settings in one document.

## Expression rule split

`expression` selects only field-value checks. Severity settings, `lint.rules.disable`, `ignores` and inline suppressions match exact IDs.

| Diagnostic                           | Rule ID                |
| ------------------------------------ | ---------------------- |
| Field values                         | `expression`           |
| Parsing                              | `expression-syntax`    |
| Undefined names and properties       | `expression-reference` |
| Invalid types                        | `expression-type`      |
| Function arguments and calls         | `expression-call`      |
| Context availability                 | `context-availability` |
| Event payload properties             | `event-payload`        |
| Known untrusted script interpolation | `script-injection`     |

An old `disable: [expression]` must list the IDs above to keep those checks off. A configured ignore needs one entry per ID. Inline comments accept comma-separated IDs, for example `# actionlint:ignore expression-reference -- reviewed name`.

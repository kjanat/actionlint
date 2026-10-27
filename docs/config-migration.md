# Configuration migration

`actionlint config migrate --config .github/actionlint.yaml` previews the
current configuration without writing. Add `--write` to replace the selected
file atomically, or `--check` to exit with status 1 when conversion is needed.
`--json` includes the converted content and migration notes.
Only the selected file is rewritten. Inherited files are normalized while read;
select each inherited file explicitly to rewrite its syntax too.

Local CLI and GitHub Action checking automatically migrate a selected legacy
file before plugin selection. Explicit `config migrate` also supplies default
ShellCheck and Ruff declarations when `plugins` was omitted. An explicit `plugins`
value, including `[]` or `null`, prevents default plugin insertion. Native backends
are selected for explicit process configuration and ShellCheck source or rc-file settings.

Applying this migration resolves and verifies plugin dependencies before saving
the config and its lockfile. A failed download leaves both project files unchanged.
Preview and `--check` do not download artifacts. Already canonical configs are
not automatically enrolled in default plugins.

The parser accepts old documents through the same conversion, including
inherited files and inline configuration. Parsing alone never writes files.
Migration writes the input format: YAML, JSON or TOML. TOML schema directives are
copied to the output; comments within moved settings appear before the resulting `lint` section.
A prepared file migration checks its entire inherited input graph before writing;
concurrent edits abort the write.
TOML cannot express null resets. If encoding would change inherited settings,
migration refuses the rewrite; convert that selected document to YAML or JSON first.

| Old setting                  | Current setting                                                    |
| ---------------------------- | ------------------------------------------------------------------ |
| `self-hosted-runner.labels`  | `lint.rules.correctness.runner-label.options.labels`               |
| `config-variables`           | `lint.rules.correctness.expression-reference.options.variables`    |
| `config-secrets`             | `lint.rules.correctness.expression-reference.options.secrets`      |
| `assume-default-permissions` | `lint.rules.correctness.workflow-call.options.default-permissions` |
| `policy.<rule>`              | `lint.rules.<group>.<rule>`                                        |
| `paths.<glob>.ignore`        | `lint.message-ignores.<glob>`                                      |

Knowledge options use `level: default` and supply information independently of
their diagnostic level. Null variable/secret lists mean unknown names; empty lists
permit no configured names. Built-in and declared reusable-workflow secrets are
added separately.

```yaml
lint:
  rules:
    correctness:
      runner-label:
        level: default
        options: { labels: [gpu] }
      expression-reference:
        level: default
        options: { variables: [], secrets: null }
    policy:
      require-job-timeout:
        level: on
        options: { min-minutes: 5, max-minutes: 30 }
  message-ignores:
    ".github/workflows/legacy.yml": ["legacy diagnostic text"]
```

Migration makes policy choices explicit. An old `false` becomes `level: off`;
it now survives `--strict` and group presets. This is an intentional precedence
change: explicit configuration wins over a preset. The migration report calls
out this change whenever a policy section is converted.

Conflicting old and current settings in the same document produce an error
naming both paths. Remove one or make them agree; migration never chooses
silently. Inheritance applies documents in order, merges mappings and replaces
lists. Regex message filters run after analysis. Prefer typed `ignores` for rule-
and location-specific exceptions.

## Expression rule split

V2 gives expression checks independent diagnostic IDs. `expression` now selects
field-value checks only. It is not an alias for the whole expression checker.
The same exact-ID selection applies to severity settings, `lint.rules.disable`,
configured `ignores`, and inline suppression comments, including overrides.

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

Review each desired severity or suppression under its reported ID. For example,
an old broad `disable: [expression]` must list the relevant IDs above to keep
those checks disabled. A configured ignore needs one entry per selected ID;
inline comments accept comma-separated IDs. To suppress only an undefined name,
use `# actionlint:ignore expression-reference -- reviewed name`.

Automatic syntax migration reports this change when a legacy document contains
an `expression` control. Review the migrated selection: the same setting can
intentionally select field-value checks in a new config.
Workflow comments are not rewritten. The separately introduced
`script-interpolation` rule adds checks for unknown string values and should be
configured independently; it is not an old rule renamed by migration.

# Configuration migration

`actionlint config migrate --config .github/actionlint.yaml` previews the
current configuration without writing. Add `--write` to replace the selected
file atomically, or `--check` to exit with status 1 when conversion is needed.
`--json` includes the converted content and migration notes.
Only the selected file is rewritten. Inherited files are normalized while read;
select each inherited file explicitly to rewrite its syntax too.

Checking reads legacy syntax through normalization without rewriting project files.
The `config migrate` command adds default
ShellCheck and Ruff declarations when `plugins` was omitted. An explicit `plugins`
value, including `[]` or `null`, prevents default plugin insertion. Native backends
are selected for explicit process configuration and ShellCheck source or rc-file settings.

When you apply the migration, actionlint resolves and verifies plugin dependencies
before saving exact canonical registry versions in the configuration. An existing project lock is retired only
after its recorded versions and digests agree with registry evidence and config is saved.
A failed resolution or download leaves config and the old lock unchanged. New locks are not created.
Preview and `--check` do not download artifacts. Already canonical configs are
not automatically enrolled in default plugins. A legacy lock's `latest` selector
becomes its recorded exact version; migration never upgrades it implicitly.

The parser accepts old documents through the same conversion, including
inherited files and inline configuration. Parsing alone never writes files.
Migration writes the input format: YAML, JSON or TOML. TOML schema directives are
copied to the output; comments within moved settings appear before the resulting `lint` section.
Before writing a migrated file, actionlint checks every configuration it inherits,
including fetching HTTPS base configurations again. If their contents have changed,
actionlint stops without writing. Hosted base configurations are read-only. Migrate
the local file to keep its remote `extends`.
TOML cannot express null resets or unknown variable values. If encoding would lose facts or change inherited settings,
migration refuses the rewrite; convert that selected document to YAML or JSON first.

| Old setting                  | Current setting                                                                                       |
| ---------------------------- | ----------------------------------------------------------------------------------------------------- |
| `self-hosted-runner.labels`  | `repository.runners`                                                                                  |
| `config-variables`           | `repository.variables` (names become keys with null values)                                           |
| `config-secrets`             | `repository.secrets`                                                                                  |
| `assume-default-permissions` | `repository.default-workflow-permissions` (`restricted` becomes `read`; `permissive` becomes `write`) |
| `policy.<rule>`              | `lint.rules.<group>.<rule>`                                                                           |
| `paths.<glob>.ignore`        | `lint.message-ignores.<glob>`                                                                         |

<a id="rule-id-migration"></a>

Rule IDs also changed. Runtime configuration and both suppression forms accept
the old IDs as aliases, except for the permission-rule names described below. Diagnostics report the current IDs. Explicit migration
rewrites rule settings and suppression selectors to the current names.

| Previous ID               | Current ID                |
| ------------------------- | ------------------------- |
| `require-commit-hash`     | `unpinned-reference`      |
| `require-job-timeout`     | `job-timeout`             |
| `require-permissions`     | `missing-permissions`     |
| `require-action-metadata` | `missing-action-metadata` |
| `disallow-suppressions`   | `forbidden-suppression`   |

`missing-permissions` also replaces `explicit-permissions` and
`missing-workflow-permissions`. Migration converts `scope: workflow` to
`require-at: workflow`, and `scope: job` to `require-at: every-job`.
An explicit selection of the former `missing-workflow-permissions` rule keeps
its warning level. New configurations report missing declarations as errors.
Update inline suppression comments to use `missing-permissions`; its previous
names are accepted only during configuration migration.

Current categories are `correctness`, `suspicious`, `security` and
`conventions`. The former `style` group maps to `conventions`; former `policy`
rules move to the category of each rule; former `external` rules follow their
registered descriptors. Category scalar settings apply to current category
membership. For example, `uncancellable-condition` is now `suspicious`, so an
old `correctness` scalar no longer selects it; set the rule explicitly if needed.
Migration expands legacy group levels using registered rule metadata. If a
legacy `plugins` group-level selection needs dynamic plugin metadata that is not
available, explicit migration fails with an error. Ordinary config loading binds
those rules after plugin registration.

`empty-job` moved from `suspicious` to `correctness` and now reports errors by
default because GitHub rejects jobs without steps or a reusable workflow call.
Run `actionlint config migrate` to move existing settings to
`lint.rules.correctness.empty-job`. Explicit severity settings, including `off`,
are retained, also in overrides.

The `required-actions` rule has been removed. Delete its configuration entries
before migrating; migration rejects remaining entries.

Older knowledge options on `runner-label`, `expression-reference` and `workflow-call`
also migrate to `repository` without changing their diagnostic levels. The repository settings
are still available when those rules are off. Null means unknown; empty collections mean
known empty sets. Built-in and declared reusable-workflow secrets are added separately.

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

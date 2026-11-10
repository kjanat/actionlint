# Configuration consumer tests

Run the matrix and effective configuration tests from the repository root:

```sh
mise exec -- run test:config-schema
```

The matrix reads fixtures and expected outcomes from `cases.json`. For each case, it decodes the configuration and validates it against the generated local JSON Schema. For TOML fixtures, it also runs `tombi lint --offline --error-on-warnings` against that schema. The test skips Tombi subtests when Tombi is unavailable. JSON cases cover root types and null values that TOML cannot express. Both the loader and schema accept a null root as default configuration.

The model check calls `ResolveValue`. Cases marked `resolve_rules` also call `ResolveRules`, which checks built-in rule options and rule registration. It does not load plugin engines or validate installed provider settings in an engine. The specialized schema uses the same relative resource ID and local definition name in common and process settings. Valid and invalid limit cases check that actionlint scopes these two resources separately.

Fixture names identify the configuration area:

- `rules-*` covers presets, levels, all four built-in option models, suppressions, aliases, legacy settings, and overrides.
- `tools-*` covers plugins, tool settings, paths, generic and installed providers, backend selection, process and wasm settings, and overrides. Installed-provider shorthand binding is tracked in [issue #371](https://github.com/kjanat/actionlint/issues/371).
- `repository-*` covers repository facts, files, output, ignores, local actions, overrides, extends, and legacy fields.

`tools-file-shorthand.toml` and `tools-file-override-shorthand.toml` reproduce issue #371. Add them to `cases.json` as accepted configurations when fixing that bug.

The three expected outcomes can differ. Deprecated aliases produce Tombi warnings, and the decoder accepts legacy fields for migration. Structural validation allows unknown provider and rule names that later registration checks can reject. Initial decoding also defers installed provider settings to their schemas and engines.

The model checks glob syntax, case-insensitive duplicates, normalized local-action aliases, and cross-field timeout limits. The schema does not express those checks. The corresponding fixtures record the model and schema outcomes separately.

`TestEffectiveConfigInheritance` checks base precedence, selected values, source order, and field origins. `TestEffectiveRuleOverrides` checks selected rule levels and options after matching overrides. These tests do not execute workflows or external tools.

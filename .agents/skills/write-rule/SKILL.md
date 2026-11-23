---
name: write-rule
description: Implement or change actionlint lint rules, including diagnostic fixes, rule renames, and external-linter manifest/jq plugins. Use for rule behavior and its configuration, schema, suppression, and regression tests. Skip this skill when the task only runs actionlint or explains a finding.
---

# Write an actionlint rule

## 1. Decide what the rule reports

Find a similar case that the rule must accept and the runtime information that separates the two cases. Cite the supporting source in a test comment or the rule documentation. For GitHub behavior, confirm whether GitHub's server or the runner enforces it with the sources in [validation.md](references/validation.md#independent-and-real-world-checks).

A rule that reports a confirmed violation must emit no finding when any fact that proves the violation is unknown. A rule that reports a potential risk must state the uncertainty in its diagnostic.

## 2. Choose the implementation path

Read the matching section of [implementation.md](references/implementation.md).

- **Native semantic check.** Define a `define.Rule` under `internal/checking/rules/<category>` and add it to `All` in `internal/checking/executor/registration`. Declare `SupportsActions` when the check applies to actions. Put analysis that several rules need in the semantic model.
- **External linter.** Write a plugin. Add no tool-specific host adapter.
- **Rename or configuration change.** Keep the enabled rules, severity levels and options the same.

| Decision           | How to decide                                                                                                                                                                           |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Category           | `correctness` for language or runtime violations, `suspicious` for a demonstrated mistake pattern, `security` for a trust or authority exposure, `conventions` for project preferences. |
| Severity           | Error for a confirmed invalid workflow or security violation, warning for a possible failure or a violated convention, info for advice.                                                 |
| Default enablement | A rule that rejects common, supported behavior needs explicit selection unless the task establishes a repository-wide requirement.                                                      |
| Maturity           | Stable only when semantics, valid counterexamples and integration behavior are verified. Otherwise nursery.                                                                             |
| Fix                | An automatic fix only when the edit preserves intended behavior. A suggestion for a deliberate behavior change.                                                                         |

Name the finding ID after the problem and leave the category out. Add a configuration option only when users must choose between supported behaviors. Ask the user only when an unspecified preference changes what the rule accepts.

## 3. Implement the rule

Implement the check on an existing semantic interface. Do not re-evaluate expressions or traverse raw YAML when the model provides the fact.

Report each diagnostic at the captured source range of the problem. Apply proposed fixes in tests and analyze the edited source. Choose tests with [validation.md](references/validation.md).

Check the diff for unsupported assumptions, repeated analysis, default changes, overbroad suppressions and fixes that touch unrelated source.

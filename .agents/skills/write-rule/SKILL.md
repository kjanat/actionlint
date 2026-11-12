---
name: write-rule
description: Implement or change actionlint lint rules, including diagnostic fixes, rule renames, and external-linter manifest/jq plugins. Use for rule behavior and its configuration, schema, suppression, and regression tests; not for merely running actionlint or explaining a finding.
---

# Write an actionlint rule

Implement the rule and test its configuration and diagnostics through actionlint.
The implementation reference identifies the interfaces for each kind of check.
Read the applicable repository instructions and task definitions before choosing
implementation, formatting, and validation commands.

## 1. Decide what the rule reports

Start from the reported workflow or action, requested convention, or scanner output.
Check whether an existing rule covers the behavior before adding another.
Reduce the input to a case that demonstrates the problem. Identify a similar case
that should be accepted and any runtime information needed to distinguish them.
Assert the expected behavior in the regression test. Cite the source that supports
it in a test comment or the rule documentation.

For GitHub behavior, identify whether GitHub's server or the runner controls the
syntax or operation being checked. Use the corresponding reference described in
[validation.md](references/validation.md#independent-and-real-world-checks).
For a project convention, identify the restriction the user requested. A rule
that reports a confirmed violation must emit no finding when the facts needed to
prove that violation are unknown. If the rule reports a potential risk instead,
explain in its documentation why the uncertainty creates that risk and state the
uncertainty in the diagnostic.

Read how the model computes the values needed by the rule and how existing checks
use them before adding new analysis.

## 2. Choose the implementation path

Read the matching section of [implementation.md](references/implementation.md):

- **Native semantic check:** define the rule's name, category, severity and
  required data in a `rule.Declaration`. Add it to `internal/checking/rule_index.go`
  and register the implementation in `internal/checking/executor/registration`.
  Report findings through the host. If several rules need the same analysis,
  implement it once in the semantic model.
- **External linter:** author a plugin using the manifest format and SDK interfaces. Test its
  output conversion through actionlint; do not add a tool-specific host adapter.
- **Rename or configuration change:** identify where the rule emits findings and
  where users select or suppress it. For renames and compatibility migrations,
  verify that the enabled rules, severity levels, and options do not change.
  For intentional configuration changes, test and document the requested difference.

Choose the rule's metadata and behavior from the cases examined above:

| Decision           | How to decide                                                                                                                                                                                                                                                                                                             |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Category           | `correctness` for violations of language/runtime requirements; `suspicious` for valid behavior with a demonstrated mistake pattern; `security` for an established trust or authority exposure; `conventions` for project preferences. A valid omission is not a runtime defect merely because the request says "require". |
| Severity           | Use error for a confirmed invalid workflow or security violation, warning for a possible failure or a violated project convention, and info for advice that identifies neither a defect nor a demonstrated risk. Explain the consequence in the rule description. Choose severity separately from category.               |
| Default enablement | Assess legitimate uses and false positives. Checks rejecting common, supported behavior need explicit selection unless the task establishes a repository-wide requirement. Compare neighboring rules' recommended/default settings; do not inherit them merely because their code is convenient.                          |
| Maturity           | Use stable only when semantics, valid counterexamples, and integration behavior are verified. Use nursery for an experimental heuristic or unresolved semantic coverage; explain what evidence is missing.                                                                                                                |
| Fix                | Offer an automatic fix only when the source edit preserves intended behavior. Use a reviewable suggestion for a deliberate behavior change. Omit the fix when no justified edit is available. Decide per finding.                                                                                                         |

Choose a finding ID that names the problem without prefixing its category.
Add a configuration option only when
users need to choose between supported behaviors; describe what each value changes.
Check the catalogue and configuration loader for accepted metadata and how presets
select rules. Provider identity does not determine category, severity, or defaults.
Explain these choices in the rule description and test the resulting behavior.
Ask the user only when an unspecified preference would change what the rule accepts.

## 3. Implement and test the rule through actionlint

First add a regression that exposes the missing or incorrect behavior. Implement
the check using an existing semantic interface that supplies the facts it needs.
A declaration and its invocations are different: include invocation context when inputs or ancestry can
change the answer. Avoid rebuilding expression evaluation or traversing raw YAML
when the model already provides the needed fact.

Use the captured source and the range of the reported problem for the diagnostic's
location. Decide whether an edit is possible for each finding. Apply proposed
fixes in tests and analyze the edited source.

Use [validation.md](references/validation.md) to choose tests for the behavior being
changed. Test the check itself and invoke it through actionlint to verify
registration, configuration, suppression, and diagnostic locations.

## 4. Audit and finish

Review the resulting diff against the original case and valid counterexample.
Look specifically for unsupported assumptions, repeated analysis, unexpected
default changes, overbroad suppressions, and fixes affecting unrelated source.
Update generated schema/reference output when its inputs changed.

Run the enabled rule on an existing workflow or action that uses the syntax or
behavior the rule checks. Inspect each finding and record the source revision
and any false positives or missed problems. Do this in addition to synthetic tests
and catalogue or schema checks.

Report the implemented behavior, tests run, comparisons with existing workflows
or reference implementations, and any unverified behavior. Follow repository
formatting and commit instructions.

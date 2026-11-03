---
name: write-rule
description: Implement or change actionlint lint rules, including diagnostic fixes, rule renames, and external-linter manifest/jq plugins. Use for rule behavior and its configuration, schema, suppression, and regression tests; not for merely running actionlint or explaining a finding.
---

# Write an actionlint rule

Deliver an executable check with justified behavior and tests through the host.
Use the repository's current contracts. This skill does not define an alternative
architecture or authorize a redesign.

## 1. Establish what deserves a finding

Start from the reported workflow/action, requested convention, or scanner output.
Find the existing rule and its tests before adding another. Reduce the case and
write down the condition being reported, a valid near miss, and any missing
runtime information. Put useful evidence in the regression test; no separate
proposal document is required.

For GitHub behavior, check the relevant documentation and runner implementation
or pinned Runny result. Distinguish server behavior from runner behavior. For a
project convention, establish the user's intended restriction instead. An unknown
value is not proof of a violation. Narrow the check if the evidence cannot support
the requested claim.

Inspect the model's value states before writing the predicate. Distinguish absent,
known, unresolved, and invalid values; expression syntax alone does not establish
uncertainty. Equivalent known values must receive the same decision whether
written literally or as constant expressions. Test that equivalence alongside a
genuinely unresolved value. Use the existing semantic accessor described in
[implementation.md](references/implementation.md), not the scalar's raw text as
a substitute for its value.

## 2. Choose the implementation path

Read the matching section of [implementation.md](references/implementation.md):

- **Native semantic check:** follow the existing declaration/fact model through
  registration and diagnostic admission. Keep new reasoning in the shared model
  when other rules need it.
- **External linter:** author a plugin using the manifest/SDK contracts. Test its
  output conversion through actionlint; do not add a tool-specific host adapter.
- **Rename or configuration change:** trace public selectors and migration as well
  as the producer. Preserve effective selections, not just equivalent-looking YAML.

Decide the rule's contract before wiring it in. Use the evidence from step 1:

| Decision           | How to decide                                                                                                                                                                                                                                                                                                             |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Category           | `correctness` for violations of language/runtime requirements; `suspicious` for valid behavior with a demonstrated mistake pattern; `security` for an established trust or authority exposure; `conventions` for project preferences. A valid omission is not a runtime defect merely because the request says "require". |
| Severity           | Match the consequence established by this finding: error for an established failure or exposure warranting failure, warning for actionable risk or an unmet project expectation, info for advisory observations. Category alone does not establish severity.                                                              |
| Default enablement | Assess legitimate uses and false positives. Checks rejecting common, supported behavior need explicit selection unless the task establishes a repository-wide requirement. Compare neighboring rules' recommended/default settings; do not inherit them merely because their code is convenient.                          |
| Maturity           | Use stable only when semantics, valid counterexamples, and integration behavior are verified. Use nursery for an experimental heuristic or unresolved semantic coverage; explain what evidence is missing.                                                                                                                |
| Fix                | Offer an automatic fix only when the source edit preserves intended behavior. Use a reviewable suggestion for a deliberate behavior change. Omit the fix when no justified edit is available. Decide per finding.                                                                                                         |

Choose a specific finding ID and the smallest useful options surface. Check the
current catalogue/loader for valid metadata and selection behavior, including
strict/experimental modes. Provider identity is independent of these decisions.
Record the reasons in the rule explanation and relevant tests. Make the decisions
from the evidence; ask only when a missing user preference actually determines
the requested behavior. Do not ask the user to design the rule for you.

## 3. Implement and exercise the complete path

First add a regression that exposes the missing or incorrect behavior. Implement
the check against the narrowest existing semantic input. A declaration and its
invocations are different: include invocation context when inputs or ancestry can
change the answer. Avoid rebuilding expression evaluation or traversing raw YAML
when the model already provides the needed fact.

Keep the diagnostic tied to captured source and its actual range. For a fix,
establish applicability per finding; apply it and analyze the edited source.
Changing intended behavior requires a reviewable suggestion, not an automatic fix.

Read [validation.md](references/validation.md) for the relevant integration checks
and comparison methods. Exercise both the semantic test and the configured host
path. A helper returning the right finding does not prove the rule is registered,
selectable, suppressible, or correctly mapped to source.

For each requested document kind, verify a finding through its public entry path.
Workflow traversal does not prove standalone-action or called-composite coverage.
If a supported path produces no finding, trace selection, callback execution,
collection, and admission before changing the predicate or declaring completion.
Use positive integration cases for every claimed entry path. A standalone-action
test cannot substitute for a workflow calling that action through `uses`.

## 4. Audit and finish

Account for defects found along the way. For a defect blocking the requested rule,
add a reproducer and fix the shared boundary, then test its other callers. Do not
work around it only for the new rule or silently drop a requested entry path.
For an unrelated confirmed defect, record the source location, minimal input,
expected/actual behavior, reproduction command, and disposition. Link an existing
issue when applicable; publish a new issue only when authorized. Otherwise keep
the evidence in the task's local notes and link it in the handoff, explicitly
identifying it as unfiled. An issue is not fixed merely because it was recorded.

Review the resulting diff against the original case and valid counterexample.
Look specifically for unsupported assumptions, repeated analysis, unexpected
default changes, overbroad suppressions, and fixes affecting unrelated source.
Update generated schema/reference output when its inputs changed.

Before calling a new rule complete, verify its chosen severity in emitted output
when enabled with `on`, with an explicit level, and under the relevant preset.
Catalogue prose and a `DefaultLevel` setting do not prove what a finding emits.
Run the generated-schema freshness check after adding its catalogue entry. Treat
an untested requested entry path or a failing regression as unfinished work to
resolve, not a limitation to append to an otherwise-complete report.

Run the enabled rule on a relevant existing workflow or action and inspect its
findings. Record the source and what that comparison established. Synthetic cases
and a green catalogue/schema check do not replace this comparison.

Report the implemented behavior, tests run, real-world/reference comparisons,
and any unverified behavior. Follow repository formatting and commit instructions.
Do not substitute this report for implementation.

When evaluating changes to this skill itself, use [the task cases](evals/cases.json).
They define observable outcomes, not claims that an evaluation has passed.

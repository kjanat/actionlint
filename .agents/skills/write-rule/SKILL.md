---
name: write-rule
description: Write, rename, or review actionlint rules, including semantic evidence, rule registration, configuration migration, diagnostics, fixes, and runner comparison tests.
---

# Writing actionlint rules

Use this workflow for built-in and plugin rules. Read the current implementation;
file names below are entry points, not a second specification. Work through the
relevant checks without creating a separate design document for every rule.

## Establish the claim first

- Describe the exact condition reported, the information required to establish
  it, and valid situations that resemble it. Separate invalid Actions syntax,
  surprising but valid behavior, security exposure, and project conventions.
- Check current GitHub documentation and the relevant implementation in
  `actions/runner`, language services, or the pinned Runny reference. Record
  upstream revisions in regression fixtures when behavior depends on them.
  SchemaStore is useful comparison material, not the execution authority.
- Reproduce the alleged problem before coding. Keep unknown runtime values
  unknown: missing evidence is not evidence of an absent value or unsafe path.
- Check source scope against invocation scope. The same composite declaration
  can execute with different inputs, permissions, matrices, and ancestry.
- Search the existing rule catalogue and semantic model before adding a rule.
  Reuse shared facts and queries rather than repeating AST walks or creating
  a parallel evaluator. Tool-specific behavior belongs in external manifests,
  transforms, or existing examples, not new host adapters.

## Choose the contract

- Use a precise diagnostic ID describing the finding. Avoid generic umbrellas
  such as `action`, implementation names, or names that overstate certainty.
- Give each rule one primary concern category. Provider, maturity, recommended
  selection, native severity, and fix availability are independent properties.
  Category membership alone does not establish exploitability or confidence.
- Keep category defaults beside their individual rule exceptions in config.
  Installed plugins must follow the same selection and validation contract.
- An opinionated rule needs useful legitimate examples and an appropriate
  default. For example, `always()` may be necessary for cancellation cleanup;
  replacing it with `!cancelled()` changes behavior.
- Derive fix capability from real implementations. A rule can fix some
  findings and not others. Each diagnostic carries only the edits available
  for that occurrence, with applicability and revision evidence.
- Treat IDs and category paths as public configuration. Renames need aliases
  or migration for settings, exclusions, suppressions, LSP, and machine output.
  Do not silently broaden a selector or reinterpret a legacy group default.

## Implementation map

- `internal/rulecatalog`: public descriptors, examples, and rule documentation.
- `internal/config`: registration, selection, options, inheritance, migration.
- `internal/config/rules`: shared setting syntax, independent of execution.
- `rule`: public rule descriptors; follow the existing registration contract.
- `internal/workflow`, `internal/action`, `internal/semantics`: established
  semantic facts and checks. Locate a neighboring rule before choosing a home.
- `diagnostic`, `source`, `internal/application`: diagnostic admission and source
  identity. Do not bypass admission or manufacture positions from message text.
- `tools/config`, `internal/editorschema`: generated and installed-plugin schemas.
- `internal/integration`, `testdata`: frontend and real workflow regressions.

Keep analysis independent of presentation and configuration parsing. Diagnostic
messages explain the specific problem; rule descriptions belong in reference
material. Preserve typed error classification and explicit evidence limits.

For fixes, use captured source revisions and precise source spans. Check quoted,
folded, multiline, escaped, and aliased YAML scalars. Preserve unrelated bytes.
Never infer safe shell replacement from text resemblance alone: shell, quoting,
expansion context, and environment availability affect the result.

## Test the claim, not the implementation

Build a small table of positive, negative, and unknown cases. Include:

- the user's original case and a minimal reproduction;
- valid counterexamples, including intentional unusual syntax;
- the same declaration under different invocation inputs;
- dynamic and missing values, matrices, reusable calls, and composite actions;
- category default, explicit rule exception, disable, strict/experimental
  selection, inherited configuration, and file overrides;
- suppression matching and unused-suppression accounting;
- exact diagnostic code, source, range, severity, and applicable fixes.

Apply a proposed fix and analyze the resulting source again. Assert both that
the target finding is addressed and that unrelated source and semantics survive.
Behavior-changing suggestions need explicit applicability, not a safe-fix label.

For renames and category changes, compare effective settings before and after
migration, including options and overrides. Exercise YAML, JSON, and TOML;
conflicting aliases, null resets, unknown plugin metadata, and repeated migration.
Validate both the generated schema and the runtime configuration loader.

## Independent comparisons and final audit

- Replay relevant fixtures through the pinned Runny runner implementation or
  the matching upstream parser/evaluator. Compare behavior, not just whether
  both tools produce a message. Record disagreements as counterexamples.
- Use real workflows/actions from the existing corpus, including awkward
  syntax. Keep private fixtures private. Do not download or execute arbitrary
  workflow code as part of an ordinary unit test.
- A live GitHub run is useful for server-side behavior absent from runner code.
  Use an explicitly authorized disposable test and record run/job evidence;
  ordinary test authorization does not authorize pushes or dispatches.
- Check that schema completion, CLI rule explanation, diagnostics, suppressions,
  plugin contracts, and docs agree. Generate derived files from their source.
- Run targeted tests through `mise exec -- ...`; use repository formatting tasks.
  Before a Go commit, run the required official modernizer in affected modules,
  inspect all edits, and verify final staged content. Avoid concurrent memory-heavy
  Go analyzers on this development machine.
- Report separately what source was inspected, what tests actually ran, what
  external reference was exercised, and what could not be verified. A green
  fixture does not establish every platform or every invocation is correct.

Use the current task's authorization and repository instructions for expensive
tests and external writes. Do not turn this skill into another approval loop.

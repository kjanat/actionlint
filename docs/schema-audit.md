# GitHub Actions schema audit

Audit date: 2026-09-13. The work started from detached commit [`kjanat/actionlint@adeac5c`](https://github.com/kjanat/actionlint/commit/adeac5c560843988d9be9738a73a2b708513d921) in an isolated checkout. This report separates official schema coverage, additional runner conversion rules, and retained linter policy.

## Coverage and authorities

| Report                                                                 | Scope                                                                                                                                                                           |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Workflow structure and nested definitions](schema-audit-nested.md)    | All 324 named workflow definitions across runner and Language Services: roots/jobs, triggers and enums, nested mappings/sequences/unions, required properties and scalar types. |
| [Expression contexts, functions and scalars](schema-audit-contexts.md) | Every context-bearing schema definition, 37 workflow availability keys, builtins/special functions, expression depth, scalar decoding, and whole-object expressions.            |
| [Action metadata](schema-audit-actions.md)                             | All 25 manifest definitions and 41 explicit properties, runtime variants, composite steps, expression contexts and loader constraints.                                          |

Workflow schema authorities are runner commit `759385a3510197a58b5c08dc1f373b74b9f4643b` and Language Services commit `4043eda158e16579cc5fb1b0b07a4bce2a76f0b5`. The reports link exact source files and additional merged upstream fixes. Source disagreements are recorded individually; schema acceptance alone does not establish that a runtime loader supports a form.

## Changes

Workflow support now includes root description, cancellation timeouts, correct reusable-call classification for snapshot/cancellation fields, image-version activity and scalar filters, empty choice options, disabled service images, and stacked pull-request activity. Dynamic mappings and supported string/object unions retain expressions and validate known shapes. Matrix expression types retain per-job values. Schedule entries require cron; valid UTC timezone aliases are accepted and contextless wait-all expressions are rejected; static required flags, scalar tags, step IDs and expression-depth boundaries follow runner validation.

Action metadata validation retains raw schema structure while checking Docker/JavaScript/composite/internal-plugin variants, required properties and field types. Generated manifest constraints have a pinned schema fixture. Docker lifecycle conditions and env contexts, composite scalar conversion, step IDs, insertion recursion and explicit tags are checked. Metadata input checks permit a dynamic workflow with mapping without inventing missing inputs.

## Retained differences

The detailed reports distinguish unsupported legacy/internal event names, intentional nonempty/no-op checks, numeric/boolean type strictness, source-version disagreements and opaque runtime expression values. Known conformance differences remain reviewable in [the exact diagnostic baseline](../testdata/conformance/differences.json). Runtime expression evaluation, external action behavior and GitHub-hosted workflow execution were not tested by dispatching workflows.

The conformance fixture archive is separately pinned by [sources.json](../internal/conformance/sources.json). Its runner revision is `602c0085328df8cb595fc2641d69f640a11377a4`; this is distinct from the newer runner schema audited above.

## Validation

- `go test ./...`: passed across all packages. Windows used Git Bash on PATH for shell-based tests.
- `go test -tags conformance -run '^TestUpstreamConformance' -count=1 -timeout 2m .`: passed against the pinned archives: 1,721 cases, 1,394 agreements, 327 recorded differences. This resolves 27 previous differences and records one new SchemaStore discrepancy: its composite example repeats a step ID rejected by runner conversion.
- `golangci-lint run`: passed, zero issues.
- `dprint check` on changed non-fixture sources and reports, CommentCop, and `git diff --check`: passed. The conformance JSON was also formatted and checked explicitly.
- `scripts/check-checks`: all examples passed using a temporary copy that normalized one expected missing-file error to Windows path/error wording. The canonical document has no other regeneration differences. A Linux attempt could not run the shellcheck example because shellcheck/pyflakes are absent from WSL.

These checks ran locally in the isolated checkout. No remote CI, workflow dispatch, commit or publication was performed. Investigation logs, the temporary platform-normalized document, and downloaded reference copies were removed after recording these results.

# Analysis results

CLI `check --json`, Action `format: json`, and the Action `result-file` use the
same [versioned schema](../schemas/results/v1.schema.json). JSONL (`jsonl` in the
CLI, `json-lines` in the Action) emits the same diagnostic objects, each with
`schema_version: 1`. SARIF and command-specific metadata have separate purposes
and keep their own formats. Explicit custom templates still control their output.

```json
{
  "schema_version": 1,
  "status": "success",
  "completed": true,
  "exit_code": 0,
  "file_count": 1,
  "diagnostics": [],
  "configurations": [],
  "hints": []
}
```

| Status            | Completed | Exit code | Meaning                      |
| ----------------- | --------- | --------- | ---------------------------- |
| `success`         | `true`    | 0         | No finding requires failure  |
| `problems-found`  | `true`    | 1         | A finding requires failure   |
| `invalid-options` | `false`   | 2         | Rejected arguments or inputs |
| `failure`         | `false`   | 3         | Could not complete the check |

`file_count` is the selected workflow count; `null` means unknown, and `0` means
none selected. A failed result can include findings collected before the error; an empty list on a
failed run does **not** mean the inputs are clean. `error` explains failure.
`configurations` records selected files, overrides, and available origins and
warnings. `hints` contains advice. Producers may attach a `sarif` report.
The Action includes SARIF only when `format: sarif` or `sarif: true` requests it.

`action_metadata` records `available`, `unavailable` or `invalid` evidence for
distinct remote references requested by metadata-dependent checks. Unavailable
entries carry a reason; available and invalid entries identify the selected
repository, commit and manifest. Invalid entries include manifest problems;
their coordinates refer to that manifest, with zero meaning unavailable.
An empty list does not establish complete coverage. See
[remote action metadata](remote-actions.md) for acquisition and cache limits.

Diagnostics contain `rule`, `message`, `path`, `start`, and `end`. Optional `code`
and `severity` preserve analyzer metadata; `snippet` contains source text. Optional
`fixes` describe alternatives, each containing edits that must be applied together.
Dependency findings with `non_blocking: true` appear in the report without
changing the exit code. A successful result can therefore contain findings.
The CLI skips fixes that edit remote files when applying changes to disk.
Positions are one-based Unicode code points, with exclusive ends. Paths are
relative to the analysis working directory where possible. Do not use array
indexes as stable identities.

Related locations may include `revision: {document, digest}` and `coordinates`
(`yaml` or `raw-lf`). Supply both fields together or omit both. The `document`
identifies an absolute path or the reserved `<stdin>` source, independently of
the path shown to the user. Before reporting a location, the host checks that
its digest matches the captured source, its coordinate model matches that
source, its display path identifies that document, and its entire range is
valid. It cannot verify locations against sources that failed to load, have
conflicting revisions, are unavailable, or contain invalid UTF-8. It omits each
invalid related location and keeps the primary diagnostic. An
`admission-rejected` error reports the number of omitted locations and marks the
check incomplete.

When checking completes, the CLI writes results to stdout or `--output-file`,
including results with findings. If checking fails to complete, it reports the
failure on stderr and leaves an existing output file unchanged. Logs and opt-in
summaries on stderr use separate records. Argument errors before an operation
is selected use the CLI error record (`error` and `exit_code`). JSONL contains findings only: use
the process exit code or the Action `result-file` to distinguish completion from
failure. Action `fail-on-error: false` lets a step with findings pass. The result
records the analysis exit code regardless of that setting.

## Document outlines

The optional `documents` array describes workflows and action manifests parsed
during the check, with source positions where available. It leaves out details such
as script bodies and expression trees. The `kind` field distinguishes workflows
from action manifests. For example:

```json
{
  "documents": [
    {
      "kind": "workflow",
      "path": ".github/workflows/test.yml",
      "name": "Test",
      "parse_status": "complete",
      "triggers": [
        "push"
      ],
      "jobs": [
        {
          "id": "build",
          "start": {
            "line": 5,
            "column": 3
          },
          "needs": [],
          "steps": [
            {
              "kind": "uses",
              "uses": "actions/checkout@v6"
            },
            {
              "kind": "run",
              "name": "Test"
            }
          ]
        }
      ]
    },
    {
      "kind": "action",
      "path": "action.yml",
      "name": "Check",
      "parse_status": "complete",
      "inputs": [
        {
          "id": "target",
          "default": ".",
          "required": false
        }
      ],
      "outputs": [],
      "runs": {
        "kind": "javascript",
        "using": "node24",
        "main": "action.mjs"
      }
    }
  ]
}
```

Jobs include their IDs, dependencies, available names and source positions, and
reusable-workflow `uses` references. Steps include available IDs, names, positions,
and action `uses` references. Jobs follow source order (ID breaks position ties);
steps follow declaration order. Positions use the same convention as diagnostics
and are omitted when unavailable. Paths match diagnostic paths.

Action documents include their description, declared inputs and outputs, and
`runs` declarations. Runtime labels group observed composite steps, JavaScript
entrypoints and conditions, Docker images/entrypoints/arguments, plugins, and
unknown declarations. They carry no guarantee of runner support or executability.
Inputs and outputs include descriptions and source positions when available; inputs
also expose `required` and `default`, and outputs expose declared `value`.

Actions appear when a selected input is an action manifest or checking reads a
called action's metadata. Directory inputs select action manifests and workflows
under `.github/workflows`, subject to discovery and configuration exclusions.

Jobs and steps include the parsed `uses` string and may include a structured
`reference`: repository, workspace, self-repository, container, builtin, or
unknown. A step's `./` action is a workspace reference. A job's `./` workflow
call is a self-repository reference, like `$/`, because GitHub resolves it from
the caller's commit. Repository references separate owner, repository, subpath, and ref.
`host_source` records whether the host comes from the default context, an
explicit declaration, or the calling repository. A bare `owner/repo@ref` leaves
the host and provider unspecified. Explicit URLs include their declared host and
scheme. These fields describe the written reference; they do not resolve it,
establish that its workflow syntax is supported, or identify the runtime or
provider that would execute it.

`parse_status` records the existing parser or decoder's outcome: `complete` means
it returned a document tree without errors, `partial` means a document tree
was recovered despite errors, and `failed` means parsing produced no tree. Workflow and action parsers perform
different grammar checks. A `complete` outline can omit declarations, describe
invalid input, or reference files that cannot execute. `diagnostics` reports validity
findings; the report's `completed` tracks analysis completion separately.
Lists are always arrays, including when empty. Older producers may omit
`documents` entirely.

Known step kinds are `run`, `uses`, `wait`, `cancel`, `parallel`, and `unknown`;
consumers must tolerate new kinds. Parallel steps contain nested `steps`. Script
bodies, expression trees, and resolved relationships are not included. Composite
action declarations have their own documents; steps are not expanded into each
caller. The npm package exports `DocumentOutline`, `WorkflowOutline`,
`ActionOutline`, runtime/input/output types, `UsesReference`, `JobOutline`, and
`StepOutline` alongside `CheckResult`. These report types cover selected
declarations and source locations. They do not expose the complete parsed syntax
tree or the information supplied to executing plugins.

## TypeScript and compatibility

```typescript
import type {
  CheckResult,
  CheckResultV1,
  DiagnosticRecord,
} from '@kjanat/actionlint/result';

function describe(result: CheckResult): string {
  if (!result.completed) return result.error ?? result.status;
  return `${result.diagnostics.length} findings`;
}
```

The npm package includes the types and schema for its matching binary version.
`CheckResult` describes the currently emitted result; `CheckResultV1` explicitly
selects version 1. Import the schema as `@kjanat/actionlint/result.schema.json`
or `@kjanat/actionlint/schemas/results/v1.schema.json`. TypeScript types alone do
not validate parsed JSON.

Consumers must ignore unknown properties. Optional fields and new rule IDs,
analyzer codes, or severities can be added within version 1. Removing fields,
changing their types or meaning, or adding an incompatible outcome requires a
new `schema_version`. Schema descriptions and compatible corrections can evolve
without bumping the contract version. Use the schema shipped with your package
when reproducibility matters.

## Migration

Action `format: json` now returns the result object; read findings from
`result.diagnostics`. Action JSONL now matches CLI JSONL. Diagnostic fields change
from `kind` to `rule`, `filepath` to `path`, and `line`/`column` to `start`.
`end_column` becomes `end.column`. Ends are now **exclusive**: add one when converting
an old inclusive end column. Ranges can span multiple lines. `snippet` contains source text.

CLI JSON and the Action `result-file` use the version 1 result contract.
User-selected Go templates, including `-format '{{json .}}'`, define custom output
outside the public analysis-result API.

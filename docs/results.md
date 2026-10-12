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
| `success`         | `true`    | 0         | Finished without findings    |
| `problems-found`  | `true`    | 1         | Finished with findings       |
| `invalid-options` | `false`   | 2         | Rejected arguments or inputs |
| `failure`         | `false`   | 3         | Could not complete the check |

`file_count` is the selected workflow count; `null` means unknown, and `0` means
none selected. Failed results can retain known findings, but an empty list on a
failed run does **not** mean the inputs are clean. `error` explains failure.
`configurations` records selected files, overrides, and available origins and
warnings. `hints` contains advice. Producers may attach a `sarif` report.
The Action includes SARIF only when `format: sarif` or `sarif: true` requests it.

Diagnostics contain `rule`, `message`, `path`, `start`, and `end`. Optional `code`
and `severity` preserve analyzer metadata; `snippet` contains source text. Optional
`fixes` describe alternatives, each containing edits that must be applied together.
Positions are one-based Unicode code points, with exclusive ends. Paths are
relative to the analysis working directory where possible. Do not use array
indexes as stable identities.

Successful CLI results go to stdout or `--output-file`. Failed CLI checks emit
the result on stderr and preserve an existing output file. Logs and opt-in
summaries on stderr remain separate records. Argument errors before an operation
is selected use the CLI error record (`error` and `exit_code`). JSONL contains findings only: use
the process exit code or the Action `result-file` to distinguish completion from
failure. Action `fail-on-error: false` lets a step with findings pass. The result
retains the analysis exit code.

## Document outlines

The optional `documents` array is a lossy report projection of observed workflow
and action declarations, with available source positions. `kind` distinguishes
the two document shapes. For example:

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
Inputs and outputs retain available descriptions and source positions; inputs
also expose `required` and `default`, and outputs expose declared `value`.

Actions appear when analysis reads their metadata. The CLI still selects
workflows; it does not scan every `action.yml` in a repository. Go callers can use
`ParseActionOutline` to parse an action manifest directly.

Jobs and steps retain the parsed `uses` string and may include a structured
`reference`: repository, workspace, self-repository, container, builtin, or
unknown. A step's `./` action is a workspace reference. A job's `./` workflow
call is a self-repository reference, like `$/`, because GitHub resolves it from
the caller's commit. Repository references separate owner, repository, subpath, and ref.
`host_source` records default, explicit, or self context; a bare `owner/repo@ref`
does not imply a provider or invent a host. Explicit URLs retain their host and
scheme when available. Dependency resolution is outside this model.
Reference classification establishes neither dialect support nor an authoritative
runtime or provider identity.

`parse_status` records the existing parser or decoder's outcome: `complete` means
it returned a document tree without errors, `partial` means a document tree
survived errors, and `failed` means no tree was retained. Workflow and action parsers perform
different grammar checks. A `complete` outline can omit declarations, describe
invalid input, or reference files that cannot execute. Validity findings remain
in `diagnostics`; the report's `completed` tracks analysis completion separately.
Lists are always arrays, including when empty. Older producers may omit
`documents` entirely.

Known step kinds are `run`, `uses`, `wait`, `cancel`, `parallel`, and `unknown`;
consumers must tolerate new kinds. Parallel steps contain nested `steps`. Script
bodies, expression trees, and resolved relationships are not included. Composite
action declarations have their own documents; steps are not expanded into each
caller. The npm package exports `DocumentOutline`, `WorkflowOutline`,
`ActionOutline`, runtime/input/output types, `UsesReference`, `JobOutline`, and
`StepOutline` alongside `CheckResult`. These report types cover selected
declarations and source locations. A canonical AST or plugin execution model
would require a separate contract.

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
`CheckResult` names the currently emitted contract; `CheckResultV1` explicitly
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

CLI JSON retains `schema_version` and `diagnostics`, adding status and context.
The Action `result-file` retains its version 1 contract. User-selected Go
templates, including `-format '{{json .}}'`, remain explicit custom output;
they are not the public analysis-result API.

# Analysis results

CLI `check --json`, Action `format: json` and the Action `result-file` use the same [versioned schema](../schemas/results/v1.schema.json). JSONL (`jsonl` in the CLI, `json-lines` in the Action) emits the same diagnostic objects, each with `schema_version: 1`.

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

An empty `diagnostics` list on a failed run says nothing about the inputs. `error` explains the failure. `file_count` is `null` when the count is unknown. The Action includes a `sarif` report only when `format: sarif` or `sarif: true` requests it. `action_metadata` records [remote action metadata](remote-actions.md#coverage-and-enforcement) coverage.

Diagnostics contain `rule`, `message`, `path`, `start` and `end`. Each entry in `fixes` is an alternative whose edits apply together. The CLI skips fixes that edit remote files. Findings with `non_blocking: true` leave the exit code unchanged. Positions count Unicode code points from one, and ends are exclusive.

JSONL contains findings only. Use the exit code or the Action `result-file` to detect failure. With Action `fail-on-error: false`, a step with findings passes, and the result still records the analysis exit code.

## Document outlines

The optional `documents` array outlines the parsed workflows and action manifests:

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
    }
  ]
}
```

`parse_status` is `complete`, `partial` when the parser recovered a tree despite errors, or `failed`. A `complete` outline can still describe invalid input. Positions follow the diagnostic convention and are omitted when unknown.

Known step kinds are `run`, `uses`, `wait`, `cancel`, `parallel` and `unknown`. Consumers must tolerate new kinds. A `uses` string can carry a structured `reference` of kind repository, workspace, self-repository, container, builtin or unknown. A job's `./` workflow call is a self-repository reference, like `$/`. Outlines omit script bodies and do not expand composite steps into callers.

## TypeScript and compatibility

```typescript
import type { CheckResult } from '@kjanat/actionlint/result';

function describe(result: CheckResult): string {
  if (!result.completed) return result.error ?? result.status;
  return `${result.diagnostics.length} findings`;
}
```

The npm package ships the types and the schema for its binary version. Import the schema as `@kjanat/actionlint/result.schema.json`. Consumers must ignore unknown properties. Version 1 can gain optional fields, rule IDs, analyzer codes and severities. Removing or changing a field requires a new `schema_version`.

## Migration

Read findings from `result.diagnostics`. `kind` becomes `rule`, `filepath` becomes `path`, `line`/`column` becomes `start`, and `end_column` becomes `end.column`. Add one to an old inclusive end column.

Go templates such as `-format '{{json .}}'` produce custom output outside this API.

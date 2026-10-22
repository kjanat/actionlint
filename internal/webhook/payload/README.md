# Webhook payload observations

`catalogue.json` is generated from `@octokit/openapi-webhooks-types` (the installed version is recorded in the root npm lockfile). That package derives its declarations from GitHub's webhook OpenAPI specification through [octokit/openapi-webhooks](https://github.com/octokit/openapi-webhooks). The catalogue records the package version and SHA-256 of its input declarations. The upstream MIT notice is retained in `LICENSE.octokit`.

Regenerate after installing the locked dependencies:

```sh
node internal/webhook/payload/generate/main.mts
```

The generator uses the TypeScript type checker to resolve references, intersections, unions, arrays and index signatures. It reads operation request bodies, not REST response schemas. Formatting runs before the generated file is written. Ordinary Go builds and analysis use the embedded catalogue and require neither Node nor network access.

The catalogue contains observations: properties, optionality, descriptions, literal values and alternatives. It does not enumerate which triggers Actions permits. That is the responsibility of the workflow event checks. Actions-specific adaptations live in `internal/workflow/eventpayload`, separately from the generated observations.

The projection in `internal/workflow/eventpayload` checks known object members, preserves array element types and combines alternatives selected by the workflow's triggers and activity filters. Optional or nullable members retain their non-null shape because Actions permits missing-property access. Free-form objects keep their index signatures. Unknown events or activity variants produce an open payload; an omitted upstream variant is not evidence that the syntax is invalid. A recursive schema edge is also open rather than infinitely expanded.

Expression diagnostics and editor completions consume this same projection. This is structural typing, not a proof that an optional field exists during a particular run. Conditions on `github.event_name` or `github.event.action` do not yet narrow the payload within a branch. Reusable workflows have an open event payload because it is inherited from their caller.

The Go package exposes isolated observations through `Variants` and `Node`. It has no dependency on expression, workflow, configuration or lint packages. Workflow aliases, event/activity selection, type projection and Actions-specific overlays belong to `internal/workflow/eventpayload`.

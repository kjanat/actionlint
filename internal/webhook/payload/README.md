# Webhook payload observations

`catalogue.json` is generated directly from the webhook OpenAPI document in [octokit/openapi-webhooks](https://github.com/octokit/openapi-webhooks), which processes GitHub's official REST API description. `generate/source.json` pins the input commit, URL and SHA-256. The embedded catalogue records those same values. The upstream MIT notice is retained in `LICENSE.octokit`.

Regenerate with Node and dprint installed:

```sh
node internal/webhook/payload/generate/main.mts
```

The generator downloads only the pinned document with a 30-second deadline and rejects a digest mismatch. Pass a local copy as its first argument to regenerate offline. Updating upstream requires changing the pin deliberately, regenerating, and reviewing trigger reconciliation differences.

The converter reads webhook operation request bodies, retains local reference edges, `allOf` intersections, `oneOf`/`anyOf` alternatives, arrays, required properties, null alternatives and explicit dynamic mappings. It has no TypeScript compiler or npm dependency. Formatting runs before the generated file is written. Ordinary Go builds and analysis use the embedded catalogue and require neither Node nor network access.

The catalogue contains observations: properties, optionality, descriptions, literal values and alternatives. It does not enumerate which triggers Actions permits. That is the responsibility of the workflow event checks. Actions-specific adaptations live in `internal/workflow/eventpayload`, separately from the generated observations.

`additionalProperties` records an explicit source policy (`allowed`, `forbidden`, or `schema`); its absence retains JSON Schema's default allowing additional properties. `mapped` records dynamic value shapes. The Actions projection deliberately checks named declarations against their documented fields even when their schema leaves additional properties unspecified; this lint policy is distinct from JSON Schema validation. Bare objects and explicit dynamic mappings remain open.

The projection in `internal/workflow/eventpayload` checks known object members, preserves array element types and retains alternatives selected by the workflow's triggers and activity filters. Omitted `types` uses the trigger registry's documented defaults, not every webhook activity. Optional or nullable members retain their non-null shape because Actions permits missing-property access. Free-form objects keep their index signatures. Unknown events or activity variants produce an open payload; an omitted upstream variant is not evidence that the syntax is invalid. A recursive schema edge is also open rather than infinitely expanded.

Expression diagnostics and editor completions consume this same projection. This is structural typing, not a proof that an optional field exists during a particular run. Equality guards on `github.event_name` and `github.event.action` narrow the payload in short-circuit expression branches. Reusable workflows have an open event payload because it is inherited from their caller.

The Go package exposes isolated observations through `Variants` and `Node`. It has no dependency on expression, workflow, configuration or lint packages. Workflow aliases, event/activity selection, type projection and Actions-specific overlays belong to `internal/workflow/eventpayload`.

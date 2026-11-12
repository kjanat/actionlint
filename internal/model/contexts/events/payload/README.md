# Webhook payload observations

`catalogue.json` is generated directly from the webhook OpenAPI document in [octokit/openapi-webhooks](https://github.com/octokit/openapi-webhooks), which processes GitHub's official REST API description. [`source.json`](../../../../../tools/generate/webhook-payloads/source.json) pins the input commit, URL and SHA-256. The embedded catalogue records those same values. The upstream MIT notice is included in `LICENSE.octokit`.

Regenerate with the configured Node and kprint versions:

```sh
mise run generate:payload
```

The generator downloads only the pinned document with a 30-second deadline and rejects a digest mismatch. Pass a local copy as its first argument to regenerate offline. Updating upstream requires changing the pin deliberately, regenerating, and reviewing trigger reconciliation differences.

The converter reads webhook request-body schemas. The catalogue includes their local references, `allOf` intersections, `oneOf`/`anyOf` alternatives, array elements, required properties, null alternatives and explicit dynamic mappings. The generator formats the catalogue before writing it. Go checks read the embedded catalogue.

The catalogue contains observations: properties, optionality, descriptions, literal values and alternatives. It does not enumerate which triggers Actions permits. That is the responsibility of the workflow event checks. Actions-specific adaptations live in `internal/model/contexts/events`, separately from the generated observations.

`additionalProperties` records the source schema's explicit setting: `allowed`, `forbidden`, or `schema`. If it is absent, JSON Schema allows additional properties. `mapped` describes values for dynamically named properties. Actionlint checks named objects against their documented fields even when the schema does not specify `additionalProperties`. This is a lint check rather than JSON Schema validation. It allows undeclared properties on bare objects and objects with explicit dynamic mappings.

`internal/model/contexts/events` builds expression types from the catalogue for the workflow's triggers and activity filters. These types describe known object properties, array elements and the possible event variants. When `types` is omitted, the trigger registry supplies the documented defaults. Optional or nullable properties use their non-null type because Actions permits access to missing properties. Free-form objects allow dynamically named properties. Unknown events, unknown activities and recursive schema references allow further properties or values rather than reporting them as invalid or expanding a recursive schema indefinitely.

Expression checks and editor completions use these same types. A type can describe an optional property without proving that the property exists in a particular run. Equality checks on `github.event_name` and `github.event.action` restrict the possible payload types in short-circuit expression branches. A reusable workflow allows unknown event properties because it inherits the event from its caller.

`Variants` and `Node` return copies of the catalogue records. This package does not import expression, workflow, configuration or lint packages. `internal/model/contexts/events` handles workflow event aliases, event and activity selection, conversion to expression types, and Actions-specific additions.

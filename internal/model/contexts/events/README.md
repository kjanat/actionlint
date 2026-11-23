# Webhook payload observations

The generator builds `payload_catalogue.json` from the webhook OpenAPI document in [octokit/openapi-webhooks](https://github.com/octokit/openapi-webhooks). `LICENSE.octokit` holds the upstream MIT notice.

```sh
mise run generate:payload
```

The generator rejects a digest mismatch. Pass a local copy as its first argument to regenerate offline. To update upstream, change the pin, regenerate and review the trigger reconciliation differences.

`additionalProperties` records the explicit setting of the source schema: `allowed`, `forbidden` or `schema`. `mapped` describes values for dynamically named properties. actionlint checks named objects against their documented fields even when `additionalProperties` is absent. Bare objects and objects with dynamic mappings allow undeclared properties.

This package builds expression types from the catalogue for the triggers and activity filters of a workflow. Without `types`, the trigger registry supplies the documented default activities. Optional and nullable properties use their non-null type. Unknown events, unknown activities and recursive schema references allow any further property.

Equality checks on `github.event_name` and `github.event.action` narrow the payload type in short-circuit branches. A reusable workflow allows unknown event properties.

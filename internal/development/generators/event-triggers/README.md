# generate-webhook-events

Generate the trigger registry [`webhooks.gen.go`](../../../model/contexts/events/webhooks.gen.go) offline:

```sh
go run ./internal/development/generators/event-triggers --registry internal/model/contexts/events/webhooks.gen.go
go test ./internal/development/generators/event-triggers
```

- The documentation tables win on conflicts. A `preserve_schema` exception keeps an activity from the schema, such as `image_version` activities and pull-request `stacked`.
- The snapshot targets GitHub.com. The generator skips GHES-only table rows. An unknown table condition fails generation.
- Without `types`, `pull_request` and `pull_request_target` select `opened`, `synchronize` and `reopened`. Other events select all permitted activities.
- Webhook-only activities never add trigger permissions. Missing, incomplete and stale exceptions fail generation and tests.

Review each reported difference against the Actions documentation. Do not accept all webhook activities mechanically.

## Documentation audit

The HTML parser reads webhook names and activity types from the tables of [the events page](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows) and prints a Go map. It writes `Not applicable` as `[]string{}` and `Custom` as `nil`. Its output differs from the canonical registry. Never write it to `webhooks.gen.go`.

Print the map:

```sh
go run ./internal/development/generators/event-triggers -
```

Read a local HTML file:

```sh
go run ./internal/development/generators/event-triggers ./input.html -
```

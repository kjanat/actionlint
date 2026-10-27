# generate-webhook-events

Generates the Actions trigger registry [`webhooks.gen.go`](../webhooks.gen.go), offline:

```sh
go run ./internal/webhook/generate --registry internal/webhook/webhooks.gen.go
go test ./internal/webhook/generate
```

`sources/manifest.json` pins full source revisions and SHA-256 digests. The primary
machine-readable input is GitHub's `actions/languageservices` workflow schema,
following `workflow-root-strict` -> `on-strict` -> `on-mapping-strict`. The permissive
schema is never used. Event configuration keys and activity enums are resolved
through the referenced `one-of`, `mapping`, and `sequence` definitions.

The pinned Actions documentation activity tables take precedence on conflicts,
with explicit `preserve_schema` exceptions for reviewed compatibility supplements.
Those exceptions preserve `image_version` activities and pull-request `stacked`
from the strict workflow schema. Payload-only activities cannot use this policy.
The pull-request table includes its pinned reusable Markdown fragment. This
snapshot targets GitHub.com (fpt/ghec).
For conditional table rows, GHES-only branches are skipped in favor of the
GitHub.com `else` variant; unknown table conditions fail generation for review.
The manifest distinguishes omitted-`types` defaults from permitted activities.
Its default source is the documentation's pull-request and pull-request-target
sections; both select opened, synchronize and reopened. Other events select all
permitted activities, including the actionless and arbitrary-dispatch cases.

Generation reconciles three sources: strict workflow schema, Actions docs, and
the separately pinned OpenAPI payload catalogue. Webhook-only actions never
expand trigger permissions. Every difference needs an exact manifest exception
with rationale, pinned source, product/version scope and production verification
status. Missing, incomplete and stale exceptions fail generation and tests.
The fixtures are public specifications; none asserts GitHub server validation was
tested. Payload aliases and special Actions payloads stay in the workflow adapter.

To update, fetch the three source files at explicit commits, update their hashes
and the payload provenance, and run generation. Review each reported difference
against Actions docs before changing exceptions. Do not mechanically accept all
webhook activities. Normal tests and generation perform no network requests.

The workflow schema is from the MIT-licensed
[actions/languageservices](https://github.com/actions/languageservices); documentation
is from [github/docs](https://github.com/github/docs), licensed CC BY 4.0 for content.
The manifest records exact source URLs and revisions for attribution.

## Documentation audit

The HTML parser provides an independent documentation audit.
Its output is not the canonical registry and must not overwrite `webhooks.gen.go`.

It does:

1. Fetch [the GitHub Docs HTML page](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)
2. Parse the HTML and find webhook names and their activity types from tables
3. Generate mappings from webhook names to their activity types as a Go map variable

## Usage

```sh
generate-webhook-events [[srcfile] dstfile]
```

Generate an independent documentation observation:

```sh
go run ./internal/webhook/generate -
```

When the HTML file is in local:

```sh
go run ./internal/webhook/generate ./input.html -
```

For debugging, specifying `-` to `dstfile` outputs the generated source to stdout:

```sh
go run ./internal/webhook/generate ./input.html -
```

## Notes

- `Not applicable` activity types are generated as an empty slice: `[]string{}`
- `Custom` activity types are generated as `nil`
- The output is sorted by webhook name so the generated file is stable

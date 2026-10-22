# generate-popular-actions

Generate the bundled [`popular_actions.json`](../../../internal/actioncatalog/popular_actions.json) catalogue.

It does:

- Fetches metadata of popular actions
  - from https://github.com
  - from JSONL file in local
- Generates the fetched data set of metadata
  - as a versioned JSON catalogue with shared definitions
  - as Go source file
  - as JSONL file

## Usage

Generate the bundled catalogue:

```sh
go run ./internal/actioncatalog/generate -f json ./internal/actioncatalog/popular_actions.json
```

Resolve registry refs to immutable commits before refreshing metadata:

```sh
go run ./internal/actioncatalog/generate -pin ./internal/actioncatalog/generate/popular_actions.json
go run ./internal/actioncatalog/generate -f json ./internal/actioncatalog/popular_actions.json
```

`-pin` uses Git's public remote refs, peeling annotated tags and caching each
repository lookup for actions in subdirectories. Failure leaves the output
registry untouched. Review the resolved commits before regenerating. Each pinned
entry fetches metadata at that commit and emits exact tag and SHA references from
the same bytes. Unknown SHAs stay unknown; actionlint never substitutes a moving
tag's metadata for an arbitrary commit. Unpinned custom registry entries remain
supported and contribute only their exact requested reference.

Detect new releases on GitHub:

```sh
go run ./internal/actioncatalog/generate -d
```

Please see output of `-help` flag for more details.

## The data source file

The data source of the popular actions is defined in [`popular_actions.json`](./popular_actions.json). This file contains an array
of each action registry. Each registry is a JSON object containing the following keys:

| Key            | Description                                                     | Example                    | Required? |
| -------------- | --------------------------------------------------------------- | -------------------------- | --------- |
| `slug`         | GitHub repository slug                                          | `"actions/checkout"`       | Yes       |
| `tags`         | Known release tags                                              | `["v1", "v2", "v3", "v4"]` | Yes       |
| `pins`         | Verified tag-to-full-commit mapping used for immutable metadata | `{ "v4": "<40 hex>" }`     | No        |
| `next`         | The next release tag. Empty means new version won't be detected | `"v5"`                     | No        |
| `path`         | Absolute path to the action from the repository root            | `"/path/to/action"`        | No        |
| `skip_inputs`  | Skipping checking inputs of this action or not                  | `true`                     | No        |
| `skip_outputs` | Skipping checking outputs of this action or not                 | `true`                     | No        |
| `file_ext`     | File extension of action metadata file. The default is `"yml"`  | `"yaml"`                   | No        |

Alternative actions registry JSON file can be used via `-r` option.

## Registry maintenance

Verify refs with `git ls-remote --tags --heads https://github.com/OWNER/REPO.git`
and fetch the action's `action.yml` or `action.yaml` at each selected ref. Major
refs may be branches. Repositories containing several actions need separate
entries with `path`; a repository name alone does not identify those actions.
Regenerate `internal/actioncatalog/popular_actions.json` with the command above after changing the registry.

## Catalogue format

The catalogue has a `$schema` link to [`popular_actions.schema.json`](../../../internal/actioncatalog/popular_actions.schema.json)
for editor completion and validation. It is separate from this directory's fetch registry.

- `version` identifies the file format; the loader rejects unsupported versions.
- `references` maps each exact action reference to a definition ID. Identical definitions share an ID,
  chosen from the first reference in sorted order. IDs do not imply version compatibility.
- `definitions` stores action names, runtimes, input/output names and validation flags. Input/output names
  preserve their casing. `required` and `deprecated` use lowercase input IDs. An empty deprecation message
  still marks an input deprecated. Missing flags default to false.
- `retired` lists exact references with retired runtimes or services.

The loader also checks relationships that JSON Schema cannot express here: definition references,
input exceptions, case-insensitive name collisions, and active/retired overlap.
Unknown action references remain unchecked; no tag or commit equivalence is guessed.

The catalogue is embedded in ordinary CLI/library builds. The playground uses the `actionlint_external_catalog`
build tag, fetches the JSON as a separate hashed asset, and verifies its SHA-256 against the WASM build before
enabling linting. Missing or mismatched metadata stops initialization. Each exported `PopularActions` entry receives independent mutable
metadata, preserving existing Go API behavior. The file shares definitions; the compatibility map does not
share mutable input/output objects between versions. Both built-in consumers use one lookup function.

`-f go` remains available for Go source consumers and `-f jsonl` for metadata interchange.

Extraction preserves supported input fields when metadata also contains unknown
input keys, such as `type` or the misspelled `deprecation-message`. Extraction ignores
those keys. Invalid values, duplicate inputs and malformed YAML
still stop generation. This does not relax validation of local action metadata.

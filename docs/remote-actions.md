# Remote action metadata

actionlint can acquire remote action manifests to check their declared inputs,
outputs and runtime. It downloads metadata by default. Use `--action-metadata=offline` to use cached
metadata without network requests, or `--action-metadata=off` to skip acquisition.

```console
actionlint check --action-metadata=online .github/workflows/build.yml
actionlint check --action-metadata=offline .github/workflows/build.yml
```

- `off` acquires no metadata. Coverage records `lookup-disabled`.
- `offline` reads previously acquired manifests for exact commits. It makes no
  HTTP requests and runs no resolver process. A missing full-SHA cache entry
  records `cache-miss`; named references record `offline` because resolving them
  requires a new observation.
- `online` uses the bundled Commitish WebAssembly plugin to resolve branches,
  tags and commit references, then downloads metadata at the selected commit.
  Annotated tags are followed to their commit. Actionlint performs the requests
  and records them in `--network-stats`.

The metadata report distinguishes these lookup failures:

| Reason                      | Meaning                                                                      |
| --------------------------- | ---------------------------------------------------------------------------- |
| `unsupported-host`          | The GitHub metadata provider cannot fetch from this host.                    |
| `unsupported-object-format` | The reference is a SHA-256 Git object ID, which the provider cannot resolve. |
| `invalid-resolver-result`   | The selected resolver returned an invalid or incomplete result.              |

When enabled, `missing-action-metadata` reports lookup failures. It does not
report disabled lookups, offline cache misses or a missing resolver.

Online mode uses `GH_TOKEN`, falling back to `GITHUB_TOKEN`, when present.
Otherwise it makes unauthenticated requests. Credentials, HTTP response bodies
and transport error details are excluded from acquisition diagnostics and cache
records. A `404` can mean that the repository or manifest does not exist or that
the caller lacks access. Actionlint reports both cases as `not-found-or-inaccessible`.

The host checks repository name/ID, reads the exact commit object, then requests
`action.yml` at that commit and action directory. It tries `action.yaml` only
after `action.yml` returns `404`. It rechecks repository identity after a new
manifest acquisition. Requests use the fixed GitHub API authority; redirects
are refused. Each response is limited to 1 MiB, each HTTP request to 15 seconds,
and the complete lookup, including an injected resolver, to 30 seconds. Caller
cancellation also stops acquisition.

The acquired data records the repository identity and manifest returned by GitHub
for the selected commit. It does not prove that GitHub would authorize a workflow
to use the action, that a runner executed these bytes, or that the commit is
reachable from a particular branch. The host uses GitHub's [commit object
endpoint](https://docs.github.com/en/rest/git/commits#get-a-commit-object) and
[repository contents endpoint](https://docs.github.com/en/rest/repos/contents#get-repository-content),
never the general commits endpoint to select between named refs.

## Cache and offline evidence

The cache lives under the operating system user cache directory at
`actionlint/action-metadata/v1`. Analysis does not create project lockfiles or
configuration. Each cache record contains the repository host, name and ID;
the full commit hash; the action directory; the manifest filename and bytes;
its SHA-256; and the observation time. The cache does not save mappings from
mutable references, such as tags or branches, to commits.

To use a cached manifest offline, specify its full SHA-1 commit hash. An online
lookup of a named reference can cache the resolved commit's manifest for a later
request using that hash. A cache entry can be reused only for the same host,
repository name and ID, and action directory. If records disagree about the
repository ID, are corrupt, or fail their digest check, the metadata is
unavailable. Offline operation cannot recheck who currently owns a repository
name. Coverage includes the recorded identity and observation time.

## Reference resolution

Commitish runs inside actionlint as a WebAssembly component. It asks actionlint
for exact references and Git objects. Actionlint fetches them, verifies the
repository identity and rechecks the observed references before accepting the
result. Credentials and HTTP responses stay in actionlint.

The Commitish library selects the reference using the action or reusable-workflow
rules. The result identifies the selected tag or commit object separately from
the commit whose files will be read. If a tag and branch share a name, the
selection follows those rules rather than GitHub's general commits endpoint.

To replace the bundled resolver, load a component implementing
`kjanat:actionlint-resolver/reference-resolver@0.1.0` with `--plugin` or a config
plugin declaration. Actionlint accepts one resolver per analysis. The plugin
requests repository data through `repository-objects@0.1.0`; actionlint limits
those requests to the repository being resolved.

`--resolve-with EXECUTABLE` selects a native resolver instead. It exchanges
version-1 JSON through stdin and stdout. `--resolver-arg VALUE` passes a literal
argument. Both routes distinguish action and workflow requests. Native resolvers
must supply their network counts for inclusion in `--network-stats`.

## Reusable workflows

The same `--action-metadata` setting controls downloads for remote reusable
workflows. actionlint reads the referenced workflow file at its resolved commit.
The `workflow-call` rule checks required and unknown inputs and secrets, then
compares the caller's known token permissions with the callee's declared job
permissions. Expression checks use the declared workflow outputs.
The recommended `missing-permissions` rule checks whether the caller declares token
permissions. By default, it also checks independently triggered workflows, but skips
workflows triggered only by `workflow_call`. Its `require-at` option can instead
require declarations at the workflow level or on every job, including reusable calls.

The bundled resolver handles named workflow references as well as commit IDs.
Offline mode accepts full SHA-1 references already cached online. Off mode
downloads nothing.

These checks inspect the directly referenced workflow. They do not follow remote
workflow call chains. Unavailable metadata supplies no input, output or permission
facts, and unknown caller permissions do not establish a permission conflict.
Remote workflow metadata is not included in the `action_metadata` results below.

## Coverage and enforcement

The analysis API exposes `ActionMetadata`; version-1 JSON results expose
`action_metadata`. Each entry describes one distinct static remote reference
requested by active metadata-dependent checks:

- `available` means actionlint parsed the manifest and its structural checks
  passed. Checks can use the declared inputs, outputs and runtime. This does not
  verify that runtime files exist or that every check which depends on those files
  completed.
- `unavailable` gives a reason for the missing metadata and supplies no metadata
  facts.
- `invalid` includes parse or schema diagnostics in the selected manifest. Their
  positions identify that manifest's repository, commit and path.

Invalid manifest diagnostics are also reported at the calling `uses` position.
This list covers only static remote action references requested by enabled
metadata-dependent checks. It does not cover local actions, Docker references,
dynamic references, reusable-workflow metadata, or transitive dependencies. An
empty list means no enabled check requested remote action metadata; it does not
mean the workflow has no remote references or that every dependency was checked.

An omitted `outputs` declaration leaves output names unknown. It does not prove
the action produces no outputs; explicit output declarations remain checkable.

Enable `missing-action-metadata` to report acquisition failures:

```yaml
lint:
  rules:
    conventions:
      missing-action-metadata: on
```

This rule is off in the recommended preset. Offline cache misses and disabled
acquisition appear in `action_metadata` coverage without a lint finding. Cached
metadata is still checked in offline mode. Repository access failures, unsupported
reference selections and invalid metadata produce findings when this rule is enabled.

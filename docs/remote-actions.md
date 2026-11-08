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
- `online` acquires GitHub.com metadata for full 40-character SHA-1 commit
  references. Named references require an injected action-profile resolver.
  The host cannot resolve SHA-256 objects or raw tag objects directly. It never
  substitutes a similarly named tag or branch for an unsupported selection.

Online mode uses `GH_TOKEN`, falling back to `GITHUB_TOKEN`, when present.
Otherwise it makes unauthenticated requests. Credentials, HTTP response bodies
and transport error details are excluded from acquisition diagnostics and cache
records. A `404` is reported as `not-found-or-inaccessible`, rather than proof
that a private repository or manifest does not exist.

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

## Resolver boundary

`--resolve-with EXECUTABLE` starts a native program and exchanges version-1
JSON requests and responses through stdin and stdout. This protocol is separate
from the WebAssembly component plugin interfaces. Action metadata sends
`kind: "action"`; reusable workflows keep `kind: "workflow"`.
Keep those request kinds separate. The provider must select references according
to the action or workflow behavior it implements. Generic Git revision parsing
or a shortcut through the GitHub API does not establish equivalent selection.

For named action metadata, a successful result must also supply
`selected_object` (the original full SHA-1 object before any tag peeling) and
`selected_ref` (the exact lookup key, such as `refs/tags/v4` or `HEAD`). It must
also include the repository ID and name, resolved commit, observation time and
evidence. Other resolver callers can use a result without these selection
fields, but metadata acquisition reports it as `unsupported`. The host
does not guess a tag or branch route. A typed `actions.Provider` can instead
supply the manifest and complete selection evidence directly to an in-process caller.

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
  positions identify that manifest's repository, commit and path, rather than
  the calling workflow.

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

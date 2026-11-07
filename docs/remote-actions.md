# Remote action metadata

Remote action metadata is optional host evidence, separate from workflow syntax
and action reference validation. The default is `--action-metadata=off`.

```console
actionlint check --action-metadata=online .github/workflows/build.yml
actionlint check --action-metadata=offline .github/workflows/build.yml
```

- `off`: no metadata acquisition. Coverage records `lookup-disabled`.
- `offline`: read previously acquired immutable manifests. No HTTP or resolver
  process runs. A missing full-SHA cache entry records `cache-miss`; named refs
  record `offline` because their selection needs a new observation.
- `online`: acquire GitHub.com metadata for full 40-character SHA-1 commit
  references. Named references require an injected action-profile resolver.
  Native SHA-256 and raw tag-object resolution are outside this host's coverage.
  They never fall back to a similarly named tag or branch.

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

These are source observations. They do not prove that GitHub would authorize
execution, that the commit is reachable from a particular branch, or that a
runner executed these bytes. The host uses GitHub's [commit object
endpoint](https://docs.github.com/en/rest/git/commits#get-a-commit-object) and
[repository contents endpoint](https://docs.github.com/en/rest/repos/contents#get-repository-content),
never the general commits endpoint to select between named refs.

## Cache and offline evidence

The cache lives under the operating system user cache directory at
`actionlint/action-metadata/v1`. Analysis does not create project lockfiles or
configuration. Cache records contain repository host/name/ID, terminal full
commit, action directory, metadata filename, bytes, SHA-256 and observation time.
Mutable ref-to-commit selections are not persisted.

Warm offline success requires an explicit full SHA-1 reference. An online named
selection can warm the corresponding terminal commit for a later explicit SHA
request. Cache entries never cross hosts, repository names/IDs or action
directories. Conflicting repository IDs for the same offline request, corrupt
records and digest mismatches are unavailable evidence. Offline operation cannot
recheck current ownership of a previously observed repository name; the recorded
identity and observation time remain visible in coverage.

## Resolver boundary

`--resolve-with EXECUTABLE` uses the existing version-1 resolver protocol. Action
metadata sends `kind: "action"`; reusable workflows keep `kind: "workflow"`.
Those profiles must remain separate. Ref selection belongs to the provider's
measured profile, not generic Git revision syntax or a GitHub API shortcut.

For named action metadata, a successful result must also supply
`selected_object` (the original full SHA-1 object before any tag peeling) and
`selected_ref` (the exact lookup key, such as `refs/tags/v4` or `HEAD`). The usual
repository ID/name, terminal commit, observation time and evidence remain
required. A result without these selection fields remains usable by existing
resolver consumers but is `unsupported` for metadata acquisition. The host
does not guess a tag or branch route. A typed `actions.Provider` can instead
supply complete manifest selection evidence directly to library callers.

## Coverage and enforcement

The analysis API exposes `ActionMetadata`; version-1 JSON results expose
`action_metadata`. Each entry describes one distinct static remote reference
requested by active metadata-dependent checks:

- `available`: parsing and structural manifest validation succeeded; input,
  output and runtime facts can be consumed.
- `unavailable`: includes a typed reason, without invented metadata facts.
- `invalid`: includes parse/schema diagnostic positions in the selected
  manifest. These positions are associated with its repository/commit/path;
  they are not positions in the calling workflow.

Invalid manifest diagnostics are also reported at the calling `uses` position.
Structural validation is not a claim that every runtime or filesystem-dependent
action check completed. Local actions, Docker references, dynamically constructed
references, reusable workflow metadata and transitive dependencies are outside
this remote-action coverage list. Disabling every metadata-dependent rule can
leave the list empty; it does not establish full coverage.

An omitted `outputs` declaration leaves output names unknown. It does not prove
the action produces no outputs; explicit output declarations remain checkable.

To require usable metadata, opt in independently of the network mode:

```yaml
policy:
  require-action-metadata: true
```

The equivalent rule is `lint.rules.policy.require-action-metadata`. It is off in
the recommended preset. Enabling the policy does not enable HTTP. Offline misses,
disabled acquisition, unsupported selections and invalid parse/schema metadata
produce a policy finding with a reason and a remedy. This replaces any notion
that membership in a bundled popular-action catalogue establishes metadata
availability.

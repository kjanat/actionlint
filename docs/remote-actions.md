# Remote action metadata

actionlint can acquire remote action manifests to check their declared inputs,
outputs, runtime and composite steps. Online checking is the default. Use
`--offline` to use cached metadata and block all network requests, including
plugin and configuration downloads.
Use `--online` to select online checking explicitly. These flags cannot be combined.

```console
actionlint check --online .github/workflows/build.yml
actionlint check --offline .github/workflows/build.yml
```

Composite steps are checked in the calling job's context. For example, a remote
composite containing a Docker action produces a `container-runner` error when
called from a Windows job. Calls to other remote composites are followed too.
Repeated calls share downloaded source files but have separate invocation records.
Diagnostics link to the manifest at the resolved commit.

Each workflow or standalone action allows up to 10 nested calls and 10,000 call
occurrences, across local and remote composites. Metadata acquisition also stops
after 10,000 distinct remote action references, including dependencies fetched
before their calls are visited. Each distinct reference counts once. Actionlint
reports an error if this metadata limit is exceeded. It does not prefetch children
of a composite at the maximum depth.

Unavailable or invalid manifests stop expansion at that call. A `./` reference inside a
remote composite resolves against the caller's checkout, as it does on the runner.
`$/` references resolve against the remote action's repository at the same commit.
Remote action files referenced by scripts are not downloaded with the manifest.

- `off` acquires no metadata. Coverage records `lookup-disabled`.
- `offline` reads cached manifests. Tags and branches use their last cached
  resolution. Missing full-SHA entries record `cache-miss`. Named references
  without a saved resolution record `offline`.
- `online` uses the bundled Commitish WebAssembly plugin to resolve branches
  and tags, then downloads metadata at the selected commit. Annotated tags are
  followed to their commit. Actionlint performs the requests and records them in
  `--network-stats`.

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

Online mode acquires each manifest in this order:

1. A full SHA-1 commit reference with a cache record returns the cached
   manifest without a network request.
2. A named reference, such as a tag or branch, reads the repository's Git
   reference list from `https://github.com/OWNER/REPO.git/info/refs?service=git-upload-pack`.
   Each repository is listed once per analysis, and every lookup of that
   repository uses the same list. Commitish selects the reference from it.
   A tag can point to a commit, tree or blob. If its target is not also a
   branch tip, actionlint checks the object type through the GitHub API.
3. Actionlint uses a cached manifest for the selected commit when one exists.
   Otherwise it requests `action.yml` at the commit and action directory from
   `https://raw.githubusercontent.com/OWNER/REPO/COMMIT/DIRECTORY/`. It tries
   `action.yaml` only after `action.yml` returns `404`. A full SHA-1 reference
   skips step 2 unless both manifest requests return `404`.
4. If the raw file host cannot answer and the repository is known to be public,
   actionlint requests the same files at the same commit from the jsDelivr CDN at
   `https://cdn.jsdelivr.net/gh/OWNER/REPO@COMMIT/DIRECTORY/`. It tries
   `action.yaml` only after `action.yml` returns `404`. jsDelivr does not serve
   private repositories. When it returns `404` for both filenames, actionlint
   continues with step 5. Actionlint caches a manifest from jsDelivr in the
   same way as one from the raw file host.
5. If the Git host cannot answer, the mirror settings prohibit jsDelivr,
   or jsDelivr does not return the manifest,
   actionlint uses the GitHub REST API instead. A host cannot answer after a
   connection failure, a timeout, a `5xx`, `401`, `403` or `429` response, an
   oversized reference list, or a response with an unexpected content type,
   such as a proxy login page. jsDelivr also fails when it answers with a
   redirect, any other status except `404`, or a manifest larger than 1 MiB.
   The REST API checks the repository name and ID, reads the exact commit
   object, requests the manifest at that commit and rechecks the repository
   identity afterwards.

Requests to `github.com`, `raw.githubusercontent.com` and `cdn.jsdelivr.net` do
not consume `api.github.com` quota. A lookup of a full SHA-1 reference makes no
request when the cache has the manifest and one request to the raw file host
otherwise. A lookup of a tag or branch makes one reference-list request per
repository, then one raw file request per distinct commit and action directory.
Tags whose target type needs verification also make API requests.
Actionlint requests files from jsDelivr only after the raw file host fails.
Use `--network-stats` to see the requests of an analysis by host and kind.

jsDelivr requests always name a full commit hash, never a tag or branch. The
raw file host also answers `404` for a private repository that the token cannot
read. After any raw file `404`, actionlint sends no request to jsDelivr. If a
later attempt fails, for example with `503`, actionlint can try the GitHub API.

The raw file host serves files only at commits. When neither manifest exists at
a full SHA-1 reference, actionlint reads the repository's Git reference list.
If the reference names an annotated tag object in that list, Commitish selects
the tag's commit and actionlint requests the manifest at that commit. If the
list contains no such tag, the lookup records `not-found-or-inaccessible`.
If the reference-list request fails, actionlint can try the GitHub API.

With a token, Git requests use HTTP Basic authentication as `x-access-token`,
and raw file and REST requests send it as a bearer token. GitHub's raw file host
answers `404` when a token is invalid, so actionlint repeats a `404` raw file
request once without the token. Requests to jsDelivr never include the token.

Redirects are refused. Each manifest is limited to 1 MiB, each reference list
to 16 MiB, each HTTP request to 15 seconds, and the complete lookup, including
an injected resolver, to 30 seconds. The 30 seconds exclude time the lookup
spends waiting for a free request slot, for another lookup's reference list,
resolution or rate-limit check, or for the resolver plugin. A lookup that
exceeds its time or a resolver time limit records `failed`. Caller cancellation
stops acquisition and the analysis. Lookups for different repositories run
concurrently, with at most 16 requests in flight.

The acquired data records the manifest that GitHub returned for the selected
commit. It does not prove that GitHub would authorize a workflow to use the
action, that a runner executed these bytes, or that the commit is reachable from
a particular branch. The REST API fallback uses GitHub's [commit object
endpoint](https://docs.github.com/en/rest/git/commits#get-a-commit-object) and
[repository contents endpoint](https://docs.github.com/en/rest/repos/contents#get-repository-content).

## Metadata mirrors

[Shared network policies](config.md#shared-defaults-and-restrictions) can exclude
repositories or prohibit jsDelivr across projects. Actionlint uses GitHub REST
when a policy prohibits the mirror request.

By default, actionlint uses jsDelivr only for repositories it has established are
public during this analysis. It learns this from a successful unauthenticated
Git or raw-file request, or the `private: false` field in a GitHub REST response.
It uses responses from requests it already needs, without an extra visibility lookup.
Authenticated Git and raw-file responses do not establish public visibility.

These are the defaults:

```toml
[network.metadata-mirrors]
services                   = ["jsdelivr"]
allow-unknown-repositories = false
```

When visibility is unknown or private, a raw-file failure sends the lookup to
GitHub REST. To use jsDelivr during that failure even when visibility is unknown,
set `allow-unknown-repositories = true`. This sends the owner, repository name,
commit hash and file path to jsDelivr. A private repository whose visibility has
not been established can be included. GitHub tokens are never sent to jsDelivr,
and known private repositories are always excluded.

To disable third-party metadata mirrors, set `services = []`. These settings
apply to remote actions and reusable workflows. They support `extends` with the
same project overrides as other configuration settings.

## Cache and offline checks

The cache lives under the operating system user cache directory at
`actionlint/action-metadata/v2`. Analysis does not create project lockfiles or
configuration. Each cache record contains the repository host and name; the
full commit hash; the action directory; the manifest filename and bytes; its
SHA-256; and the observation time. A record also contains GitHub's numeric
repository ID when the REST API fallback learned it. Manifests acquired from the
raw file host or jsDelivr have no repository ID unless the resolver supplied one.
The cache saves the selected commit and Git reference for each tag or branch.
Offline checks reuse that selection. Online checks resolve the name again.
A reference that is
the full ID of an annotated tag object is immutable, so the cache saves a second
record under that ID with the tag's commit. Later lookups of the same tag
object ID, online or offline, read that record and send no requests.

Run `actionlint cache warm .github/workflows` before disconnecting. This also
caches remote configuration and plugins needed by those workflows.
A cached manifest can be reused only for the same host,
repository name, commit and action directory. When both the record and the
lookup know the repository ID, the IDs must match. If they differ, or the record
is corrupt or fails its digest check, the metadata is unavailable. Without an ID,
actionlint cannot detect that a repository name now belongs to another
repository; the commit hash still selects the content. Coverage includes the
recorded identity and observation time.

## Reference resolution

Commitish runs inside actionlint as a WebAssembly component. It asks actionlint
for exact references and Git objects. Actionlint answers from the repository's
Git reference list, which it reads once before resolution. For objects whose
type the list does not establish, actionlint fetches the object from the API.
When the REST API
fallback is in use, actionlint fetches each reference and object from the API,
verifies the repository identity and rechecks the observed references before
accepting the result. Credentials and HTTP responses stay in actionlint.

The resolution result and the `repository` record that `repository-objects`
returns carry the numeric GitHub repository ID as an optional value. The Git
reference list does not contain an ID, so a resolution from it has none. The
REST API fallback supplies the ID. A native resolver omits `repository_id` from
its JSON result when it does not know the ID, and actionlint omits
`expected_repository_id` from the request when it has no ID to expect. When
both sides know an ID, the IDs must match. Otherwise actionlint compares the
repository names without regard to case.

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

The same `--online` and `--offline` flags control downloads for remote reusable
workflows. actionlint reads the referenced workflow file at its resolved commit,
in the same acquisition order as action manifests.
The `workflow-call` rule checks required and unknown inputs and secrets, then
compares the caller's known token permissions with the callee's declared job
permissions. Expression checks use the declared workflow outputs.
The recommended `missing-permissions` rule checks whether the caller declares token
permissions. By default, it also checks independently triggered workflows, but skips
workflows triggered only by `workflow_call`. Its `require-at` option can instead
require declarations at the workflow level or on every job, including reusable calls.

The bundled resolver handles named workflow references as well as commit IDs.
Offline mode uses cached full SHA-1 references and saved resolutions of tags and
branches. Off mode downloads nothing.

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

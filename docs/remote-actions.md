# Remote action metadata

actionlint downloads remote action manifests to check their inputs, outputs, runtime and composite steps. Online checking is the default. `--offline` uses cached metadata and blocks every network request, including plugin and configuration downloads.

```console
actionlint check --online .github/workflows/build.yml
actionlint check --offline .github/workflows/build.yml
```

actionlint checks remote composite steps in the calling job's context and follows nested remote composites. For example, a composite that contains a Docker action produces a `container-runner` error in a Windows job. Diagnostics link to the manifest at the resolved commit.

A `./` reference inside a remote composite resolves against the caller's checkout, as on the runner. `$/` resolves against the remote action's repository at the same commit. actionlint does not download script files that a remote action references.

Each workflow or action allows 10 nested calls and 10,000 call occurrences. Acquisition stops with an error after 10,000 distinct remote references.

Online mode sends `GH_TOKEN`, or `GITHUB_TOKEN` when `GH_TOKEN` is unset. Without either it makes unauthenticated requests.

Online mode reads each repository's Git reference list once per analysis from `github.com`. It then fetches `action.yml`, or `action.yaml` after a `404`, from `raw.githubusercontent.com` at the selected commit. A cached full SHA-1 reference makes no request. If the raw host fails, actionlint tries jsDelivr for public repositories and then the GitHub REST API. `--network-stats` lists the requests by host and kind.

actionlint refuses redirects. It limits each manifest to 1 MiB, each reference list to 16 MiB, each HTTP request to 15 seconds and each lookup to 30 seconds.

## Metadata mirrors

actionlint uses jsDelivr only after the analysis shows that a repository is public. jsDelivr never receives a token.

```toml
[network.metadata-mirrors]
services                   = ["jsdelivr"]
allow-unknown-repositories = false
```

`allow-unknown-repositories = true` also sends repositories of unknown visibility to jsDelivr. jsDelivr then sees the owner, repository name, commit hash and file path, which can belong to a private repository. `services = []` disables mirrors. [Shared network policies](config.md#shared-defaults-and-restrictions) can exclude repositories or prohibit jsDelivr.

## Cache and offline checks

The cache lives in `actionlint/action-metadata/v2` under the user cache directory. Run `actionlint cache warm .github/workflows` before you disconnect. The command also caches remote configuration and plugins for those workflows.

## Reference resolution

To replace the bundled resolver, load a component that implements `kjanat:actionlint-resolver/reference-resolver@0.1.0` with `--plugin` or a config plugin declaration. actionlint accepts one resolver per analysis. The plugin reads repository data through `repository-objects@0.1.0`, limited to the repository it resolves.

`--resolve-with EXECUTABLE` selects a native resolver that exchanges version-1 JSON over stdin and stdout. `--resolver-arg VALUE` passes a literal argument.

## Reusable workflows

`--online` and `--offline` also control remote reusable workflows. The `workflow-call` rule checks required and unknown inputs and secrets, compares the caller's token permissions with the callee's job permissions, and types the declared outputs. actionlint checks only the directly referenced workflow and does not follow its calls.

## Coverage and enforcement

Version-1 JSON results list each distinct static remote reference that an active check requested in `action_metadata`:

- `available` means actionlint parsed the manifest and its structural checks passed.
- `unavailable` gives a reason and supplies no metadata.
- `invalid` includes the manifest's parse or schema diagnostics. actionlint also reports them at the calling `uses`.

The list excludes local actions, Docker references, dynamic references, reusable workflows and transitive dependencies. An omitted `outputs` declaration leaves output names unknown.

Enable `missing-action-metadata` to turn lookup failures into findings. The recommended preset leaves it off. Offline cache misses and disabled lookups never produce findings.

```yaml
lint:
  rules:
    conventions:
      missing-action-metadata: on
```

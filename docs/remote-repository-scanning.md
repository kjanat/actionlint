# Remote repository scanning

`actionlint-scan` acquires an immutable repository snapshot and runs the existing
actionlint workflow and policy rules. It runs locally; no hosted scanner backend
or target checkout is required.

Build from this source tree, or install this command from a trusted revision:

```sh
go build -o actionlint-scan ./cmd/actionlint-scan
./actionlint-scan owner/repo@main > findings.json
./actionlint-scan --ref refs/pull/123/merge owner/repo
./actionlint-scan --local /path/to/checkout owner/repo@COMMIT_SHA
./actionlint-scan --config-file /trusted/policy.yaml owner/repo@COMMIT_SHA
```

Options precede the positional target. `--ref` overrides positional `@ref`.
Without a ref, the matching local checkout's HEAD or remote default HEAD is used.
An explicitly requested missing ref is an error; it never selects default HEAD.
A matching checkout supplies committed objects, including when its working tree
has modifications. Uncommitted files are not scanned.

## Acquisition and provenance

1. An existing checkout selected by `--local` or `GITHUB_WORKSPACE` is used when its
   origin matches the selected server and repository. Mutable refs resolve from
   that checkout's objects first. This supports offline scans, but local branches
   can be stale or unpublished. Choose an explicit remote commit SHA when remote
   provenance matters.
2. Otherwise REST `GET /repos/OWNER/REPO/commits/REF` resolves the ref once. An
   explicit full SHA already identifies the requested snapshot.
3. Snapshot transports are tried in order: matching local Git objects; REST
   `GET /repos/OWNER/REPO/tarball/SHA`; GitHub.com's
   `https://codeload.github.com/OWNER/REPO/tar.gz/SHA`; documented server archive
   `https://HOST/OWNER/REPO/archive/SHA.tar.gz`; Git over HTTPS; Git over SSH.
4. If REST cannot resolve a mutable ref, shallow Git fetch resolves it and the
   same fetched objects supply the snapshot. Git fetches the original requested
   ref, not a guessed branch. Once resolved, every subsequent transport uses the
   immutable SHA. Annotated refs are peeled to commits.

HTTP unavailability, authorization/rate-limit responses, missing refs and network
errors permit the next transport. Malformed successful API responses, unsafe
archives and snapshot limit violations fail closed. Cancellation stops work.
Attempts are included in the report; responses and subprocess stderr are omitted
to prevent credential exposure. An authentication failure never becomes a clean
scan. Empty/missing workflow directories are failures, matching actionlint.

The scanner constructs `NewProject(snapshot)` and calls
`LintDir(project.WorkflowsDir(), project)`. It does not invent a `.git` marker.
Repository config, local reusable workflows and local action metadata therefore
use the normal project-aware linter. Built-in policy defaults still apply;
opt-in policies come from repository config or a trusted `--config-file` override.
For independent enforcement, supply your own policy rather than relying on
configuration controlled by the scanned repository.

## Authentication and Enterprise

Public repositories work anonymously. Set `GH_TOKEN` or `GITHUB_TOKEN` for private
repositories; `GH_TOKEN` takes precedence. No token CLI flag is provided. Tokens
are sent to the selected REST/server origin. Cross-host redirects lose the
Authorization header, including sibling domains. Direct codeload is anonymous.
Redirects must remain HTTPS and cannot contain user credentials.

```sh
actionlint-scan --server-url https://github.company.example \
  --api-url https://github.company.example/api/v3 owner/repo@release
actionlint-scan --ssh-url ssh://git@ssh.company.example:2222/owner/repo.git \
  --server-url https://github.company.example owner/repo@COMMIT_SHA
```

The API URL defaults to `https://api.github.com` on GitHub.com and
`SERVER/api/v3` elsewhere. Explicit options override Actions variables. REST and
server URLs must be HTTPS, without embedded credentials, queries or fragments.
Git HTTPS uses a host-scoped header in its environment, disables redirects and
interactive credential helpers, and never puts secrets in process arguments.
Git's system/global config and inherited command overrides are disabled. Trusted
`GIT_SSL_CAINFO` and `GIT_SSL_CAPATH` remain available for Enterprise CA roots.
HTTP uses the system trust store. SSH uses `git@HOST`, batch mode, existing
known-host verification, and the user's SSH keys/agent/config. Unknown host keys
fail rather than prompting. SSH requires installed `git` and `ssh`; API/archive
transports need neither. No go-git dependency is needed.

## Checkout-free Actions use

An already-installed scanner can run directly in an Actions job without
`actions/checkout`:

```yaml
permissions:
  contents: read
steps:
  # Install actionlint-scan from a trusted source before this step.
  - name: Scan this workflow commit
    env:
      GITHUB_TOKEN: ${{ github.token }}
    run: actionlint-scan > findings.json
```

The scanner reads `GITHUB_REPOSITORY`, `GITHUB_SERVER_URL`, `GITHUB_API_URL`,
`GITHUB_WORKSPACE`, `GITHUB_SHA`, and `GITHUB_REF`. The SHA wins over the ref for
the same repository. Selecting another repository does not inherit the job's
SHA/ref. `GITHUB_GRAPHQL_URL` is intentionally not used: REST is sufficient, and
the presence of that variable does not require GraphQL. Fork PR scans need a
token with access to the selected repository/merge commit.

The existing `action.yml` Docker action and its published image are unchanged.
This command must first be built/installed; it is not yet shipped in that image
or the release archives. This feature does not publish a release or alter the
existing action's input/output contract.

## Output, limits and trust boundaries

One versioned JSON object contains `source` (repository, requested ref, SHA,
server, successful transport), `attempts`, `diagnostics` (path, line, column,
kind, message, SHA permalink), and optional `error`. Exit codes are 0 clean,
1 findings, 2 invalid options, 3 acquisition/lint/output failure. Invalid CLI
options print to stderr. A failed scan never reports successful completion.

Defaults: 2 minute overall deadline, 30 second HTTP/Git transport timeout,
256 MiB compressed archive/total file bytes, 16 MiB per file, 20,000 files.
Flags can adjust the limits, with upper caps of 4 GiB and 1,000,000 files.
Tar headers, padding and trailing decompressed content have an additional bounded
allowance. Git object storage is checked every 100 ms and after fetch, allowing
temporary overshoot within that interval; it is not a kernel disk quota. Git
fetches can download blobs unrelated to workflows before reaching these limits.

Snapshots contain regular files only. Traversal, absolute/drive/ADS paths,
Windows device names, case collisions, duplicate files, `.git` entries, symlinks,
hardlinks and special archive entries are rejected. Git trees containing symlinks
or submodules are rejected; archive services may omit submodule contents, which
are never fetched. The scanner does not execute target scripts, hooks, filters,
submodules or checkout operations. It reads raw Git blobs, so `.gitattributes`
`export-ignore` and `export-subst` do not modify Git snapshots. Remote archive
contents remain those served by GitHub; server-side export rules can differ.

Local metadata/entrypoint references cannot lexically escape the project root.
ShellCheck and pyflakes are disabled in this standalone scanner. Temporary
snapshots and fetched objects are removed on success, failure and cancellation.
No process can clean up after forced termination or machine failure. The
in-process YAML/linter work receives a cancellation context and is bounded by
input sizes; individual parser calls are not forcibly preempted by the deadline.
This is static workflow validation, not execution of or a complete security audit
of the repository. Missing local action metadata retains actionlint's existing
behavior and is not a guarantee that every local action was analyzed.

Regression tests use local HTTPS fixtures and temporary Git repositories:

```sh
go test ./internal/remotescan ./cmd/actionlint-scan
go test . -run 'TestLocalMetadataRejectsRepositoryEscape|TestLocalActionRunsFileRejectsRepositoryEscape'
```

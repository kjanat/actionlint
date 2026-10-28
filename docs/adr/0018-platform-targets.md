# 0018 — Platform targets: GitHub, Gitea, Forgejo are first-class, and portability is a lint

Status: accepted · 2026-10-28 · Gitea and Forgejo facts read from docs.gitea.com/usage/actions/comparison, forgejo.org/docs/latest/user/actions, `gitea.com/gitea/act_runner` and `code.forgejo.org/forgejo/runner` `go.mod`, `gitea.com/gitea/actionslib`, `nektos/act` `pkg/exprparser` (all fetched 2026-10-28)

## Context

The workflow syntax is no longer GitHub-only. Gitea Actions and Forgejo
Actions run the same YAML with their own runners and their own
resolution rules, and both diverge from GitHub in ways that a linter
tuned for github.com will either miss or falsely flag:

| aspect                                               | GitHub                              | Gitea                                                                                                  | Forgejo                                                                                                                                                                                          |
| ---------------------------------------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| host-less `uses: owner/repo@ref` resolves to         | github.com                          | github.com, or the instance when `[actions].DEFAULT_ACTIONS_URL = self`                                | `DEFAULT_ACTIONS_URL`, default `https://data.forgejo.org`                                                                                                                                        |
| absolute URL `uses: https://host/owner/repo@ref`     | invalid                             | accepted, any git host                                                                                 | accepted, "strongly recommended"                                                                                                                                                                 |
| extra `uses:` schemes                                | —                                   | `self:owner/repo@v1`, `builtin:`                                                                       | —                                                                                                                                                                                                |
| `$/path`                                             | same repo                           | "the repository and commit of the workflow, or of the enclosing composite action"                      | — (not documented)                                                                                                                                                                               |
| contexts                                             | `github.*`                          | `github.*`                                                                                             | `forgejo.*`, `forge.*`, `github.*` as alias                                                                                                                                                      |
| env                                                  | `GITHUB_*`                          | `GITEA_*` + `GITHUB_*`                                                                                 | `FORGEJO_*` + `GITHUB_*`                                                                                                                                                                         |
| permission scopes                                    | documented set                      | drops `statuses checks deployments id-token security-events pages`, adds `code releases wiki projects` | — (not documented)                                                                                                                                                                               |
| `jobs.<id>.environment`                              | honoured                            | ignored                                                                                                | —                                                                                                                                                                                                |
| context availability                                 | checked (ADR-0014 `context` arrays) | **not checked** — `env` works in more places                                                           | —                                                                                                                                                                                                |
| cron                                                 | 5-field                             | also `@yearly @monthly @weekly @daily @hourly`                                                         | 5-field, DST handled differently                                                                                                                                                                 |
| `workflow_call.inputs.<id>.default` may use `inputs` | per schema                          | —                                                                                                      | **no**                                                                                                                                                                                           |
| problem matchers / annotations                       | honoured                            | ignored                                                                                                | —                                                                                                                                                                                                |
| reusable workflow on another instance                | no                                  | —                                                                                                      | yes, without expansion                                                                                                                                                                           |
| expression engine                                    | `DTExpressions2` (C#, ADR-0014)     | `gitea.dev/actionslib` `pkg/exprparser` + `pkg/internal/syntax` (Go, "derived from nektos/act")        | in-tree `act/` fork: `nektos/act` `pkg/exprparser` — **lexer/parser is `github.com/rhysd/actionlint v1.7.12`**, interpreter is act's (`EqualFold` lookups, own `IsTruthy`, own `coerceToNumber`) |

The last row is the important one. On Forgejo, the thing that parses a
`${{ }}` is rhysd's Go parser — v1's ancestor — and the thing that
evaluates it is act's interpreter, not GitHub's. Three platforms, three
evaluators, one syntax.

## Decision

1. **Two axes, not one.** actup already separates them
   (`crates/core/src/provider.rs`): a provider `Kind` (which API family —
   `GitHub | GitLab | Gitea | Bitbucket | Local`) and a `host` (which
   instance — `github.com`, `ghe.corp.com`, `code.forgejo.org`), with a
   `Registry` mapping hostnames to provider instances and a
   `default_host` for host-less references. v2 adopts that split and adds
   the axis actup does not need:

   - **forge** — where the workflow *runs*: which evaluator, which syntax
     deviations. `github | gitea | forgejo`. This is what the rules key
     on.
   - **host** — where a `uses:` target is *fetched from*: hostname +
     provider kind + API base + token env. This is what `resolve_uses`
     keys on.

   They are independent: a Forgejo instance runs workflows (forge =
   forgejo) that fetch actions from `github.com` (kind = github),
   `code.forgejo.org` (kind = gitea — Forgejo's API is Gitea-compatible)
   and the instance itself.

   ```toml
   [target]
   forges = ["github", "forgejo"]  # default ["github"]

   [target.forgejo]
   default-actions-url = "https://data.forgejo.org"  # host-less uses: resolve here

   [hosts."github.com"]  # preset, zero config needed
   kind = "github"

   [hosts."code.forgejo.org"]
   kind      = "gitea"
   api_base  = "https://code.forgejo.org/api/v1"
   token_env = "FORGEJO_TOKEN"
   ```

   `[hosts."…"]` keys, `kind`/`api_base`/`token_env` names, presets for
   well-known hosts, and the "no token for a host that gets none" rule
   are actup's config verbatim, so a user with both tools writes the
   same table twice until they share one.

   With one forge, rules use that forge's facts. With several, a finding
   is reported if **any** declared forge would reject or misread the
   construct — the lint is the intersection. That is the anti-lock-in
   guarantee: a repo that declares `["github", "forgejo"]` cannot pass
   while depending on a github.com-only spelling.

   Implementation shape, from actup: a `Host` trait in `workspace`
   (`kind()`, `host()`, `cache_namespace()`, `repo_id()`,
   `resolve_ref()`, `fetch_manifest()`, `fetch_readme()`,
   `default_branch()`), one crate per kind (`host-github`, `host-gitea`,
   GitLab later) each exporting a `static REGISTRATION` with presets and
   `api_base` inference, a `Registry` built from `[hosts]`, a caching
   wrapper around every instance, deferred creation on first use. Ours is
   narrower than actup's `Provider` — read-only, network-backed with a cache in front, no
   version-set or search — and feeds commitish `Snapshot`s rather than
   doing its own resolution policy (ADR-0006). Not a dependency on
   actup's crates: the trait shape is copied, the crates converge later
   if they want to.
2. **Platform facts are data, vendored per platform**
   (`semantics/vendor/{github,gitea,forgejo}/`): `uses:` grammar and
   default host, context names and aliases, permission scopes, cron
   dialect, schema deviations (ignored keys, unchecked availability),
   env-prefix aliases. ADR-0014 is the GitHub source. The Gitea
   comparison page and the Forgejo user docs are the sources for the
   other two, with the runner repos as code of record.
3. **`UsesRef` grows a `host` component** (commitish `Pin` already
   carries host + numeric id). Grammar per platform:
   - `https://host/owner/repo[/path]@ref` — valid on gitea/forgejo,
     `uses/url-form` (correctness) on github
   - `self:owner/repo@ref`, `builtin:name` — gitea only.
     `uses/scheme-not-on-target` elsewhere.
   - host-less `owner/repo@ref` — on gitea/forgejo resolves through
     `default-actions-url`; `uses/implicit-host` (pedantic) suggests the
     fully qualified form, with a `Safe` fix when `default-actions-url`
     is configured
   - **`uses/github-host-when-portable`** (suspicious, the rule from the
     goal): with ≥2 platforms declared, a reference that only resolves
     on github.com — host-less with github as the implied host, or
     `https://github.com/...` — is reported, because the same workflow
     on the other platform fetches from a different host or fails
4. **Three expression oracles** (ADR-0017 gets two more): GitHub =
   runner `DTExpressions2`; Gitea = `actionslib` `exprparser`; Forgejo =
   act `exprparser`. ADR-0016's typing stays GitHub-semantics by default.
   Where the Go interpreters differ (truthiness, coercion, `fromJSON`
   strictness — `encoding/json` on the Go side, Json.NET on GitHub), the
   difference is a conformance fixture and an
   `expression/engine-divergent` finding when that platform is declared.
   Measuring those differences is runny's shape of work. A `runny-go`
   that drives act's and actionslib's interpreters is the way to get
   them, not reading Go by eye.
5. **Context aliases are typed as aliases**: on forgejo, `forgejo.*`,
   `forge.*`, `github.*` are one context; `expression/context-alias-not-on-target`
   when `forgejo.*` is used with only github declared.
6. **Platform-specific rules live in their normal areas** with
   `RuleMeta.targets: &[Platform]`. The registry test asserts every
   platform-conditional rule names its targets. Examples:
   `permissions/scope-not-on-target`, `events/cron-alias-not-on-github`,
   `job/environment-ignored-on-gitea`, `call/cross-instance-no-expansion`,
   `call/input-default-uses-inputs-on-forgejo`.

## Repository facts through the host

With a token, a `Host` is not just a fetcher of other people's
`action.yml`. It reads the repository the workflows belong to, and those
facts become typed inputs (ADR-0016) and rules. GitHub REST paths
verified 2026-10-28 via docs.github.com. Gitea/Forgejo equivalents are
whatever their `/api/v1` exposes and are filled in when `host-gitea` is
written.

| fact                                        | GitHub endpoint                                                                                                                                            | becomes                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| repository secrets (names)                  | `GET /repos/{o}/{r}/actions/secrets`, `…/actions/organization-secrets`                                                                                     | `secrets.*` typed as a **closed** object of known names → `secrets/unknown` (correctness) on a name that exists nowhere, `secrets/unused` (pedantic)                                                                                                                                                                                                                                                                                           |
| repository variables (names **and values**) | `GET /repos/{o}/{r}/actions/variables` (`value` is in the response), `…/actions/organization-variables`, `GET /orgs/{org}/actions/variables`               | `vars.*` typed as a closed object whose members are **string literal types** — `vars.PROVENANCE` is `'true'`, not `string`, so `expression/*` can fold conditions on it → `vars/unknown`, `vars/unused`                                                                                                                                                                                                                                        |
| environments                                | `GET /repos/{o}/{r}/environments`, `…/environments/{name}` with `protection_rules` (wait timer, required reviewers, custom) and `deployment_branch_policy` | `environment:` names typed as a literal union → `environment/unknown` (correctness); `environment/branch-policy-mismatch` when `deployment_branch_policy.protected_branches` or custom policies exclude every branch the triggering `on:` can run on; `environment/reviewers-on-automated-trigger` (suspicious)                                                                                                                                |
| environment secrets and variables           | `GET …/environments/{name}/secrets`, `…/environments/{name}/variables`                                                                                     | the `secrets.*`/`vars.*` type **narrows per job** to repo ∪ org ∪ that job's environment — a secret that exists only in `production` is `never` in a job without `environment: production`                                                                                                                                                                                                                                                     |
| rulesets                                    | `GET /repos/{o}/{r}/rulesets`, `…/rulesets/{id}`, `GET …/rules/branches/{branch}`                                                                          | rule types `workflows` and `required_status_checks` are cross-checked: `rules/required-workflow-missing` when a ruleset requires a workflow file that does not exist at the ref, `rules/required-check-unproduced` when a required status check name matches no job `name`/id in any workflow that can run on that branch. Other types (`pull_request`, `required_signatures`, `*_pattern`, …) are typed facts for future rules, not rules yet |
| self-hosted runners                         | `GET /repos/{o}/{r}/actions/runners`, `GET /orgs/{org}/actions/runners` (labels per runner)                                                                | `runner/unknown-label` gains the registered self-hosted labels, replacing v1's `self-hosted-runner.labels` config as the default source; config stays as an override                                                                                                                                                                                                                                                                           |
| default workflow permissions                | `GET /repos/{o}/{r}/actions/permissions/workflow`                                                                                                          | `permissions/exceeds-default` when a job asks for more than the repository allows, instead of guessing `assume-default-permissions`                                                                                                                                                                                                                                                                                                            |

Rules of engagement:

- All of it is optional. No token → every one of these facts is
  `unavailable` and the rules above skip, exactly as with offline
  `uses:` resolution (ADR-0006). The presence of a token never changes
  a finding that does not depend on these facts.
- Values of variables are not secrets but are repository-private. They
  live in the cache under the user's cache dir, are used only for
  typing and folding, and are **never printed** in a diagnostic
  message, a fix, SARIF, or the LSP hover. Messages say "`vars.X` is
  the literal `'true'`" only when `--show-values` is set.
- Secrets are names only. No endpoint returns values and the tool
  never asks for any.
- **Token discovery is proactive.** Per host, in order, first hit wins:
  1. `[hosts."…"] token_env` if set and non-empty
  2. the kind's conventional env vars (`GH_TOKEN`, `GITHUB_TOKEN` for
     github; `GITLAB_TOKEN` for gitlab; `GITEA_TOKEN`/`FORGEJO_TOKEN` for
     gitea)
  3. **the git credential-helper protocol**, one mechanism for every
     kind. Write `protocol=https\nhost=<host>\n\n` to the helper's
     stdin, read `password=<token>` back. Tried in this order, each as a
     subprocess with `GIT_TERMINAL_PROMPT=0`, a 5 s timeout, output
     never logged:
     - `git credential fill` — whatever helpers the user already
       registered (verified: returns `username=`/`password=` for
       github.com on this machine with no prompt)
     - the kind's CLI speaking the same protocol:
       github `gh auth git-credential get`, gitlab
       `glab auth git-credential get`, gitea/forgejo
       `tea logins helper get` (all three verified to implement the
       protocol. `tea` is `mise exec -- tea`, config under
       `$XDG_CONFIG_HOME/tea`, logins listable as JSON with
       `tea logins list -o json`)
  4. nothing → facts `unavailable`, no prompt, no browser flow

  Step 3 is why there is no per-kind token-printing command to maintain:
  the credential protocol is the stable surface, and anything the user
  has taught git (including a password manager) works unchanged.

  `[hosts."…"] token = "none"` or `--no-token` stops at step 1.
  `actionlint doctor` prints, per host, which step supplied the token
  (never the token), the scopes it carries, and which facts it can read.
- Token scopes are declared per host registration
  (`REGISTRATION.required_scopes`). A token that lacks one makes only
  the dependent facts `unavailable`, not the whole host.
- Cache keys include the repository id and a fetch time. A
  `[hosts] facts-ttl` (default 10 min for CLI, session for LSP) decides
  re-fetch.

## Consequences

- `architecture.md` gains goal 6: **portable across Actions-compatible
  forges**. GitHub stays the reference semantics. The others are
  declared, vendored deviations.
- `resolve_uses` (ADR-0006) takes the platform's default host. The
  offline cache is keyed by `(host, repo-id, sha)`, so one cache serves
  all declared platforms.
- The LSP virtual-document scheme (ADR-0013) already embeds host via
  repo-id. `actionlint:action/<host>/<repo-id>/<sha>/…` makes it
  explicit.
- Runner labels (ADR-0014 §3) are GitHub's hosted set. Gitea/forgejo
  labels are whatever the instance registers, so `runner/unknown-label`
  is `off` by default on those platforms unless `[runner] labels` is
  configured.

## Open

- Gitea `$/` semantics ("or of the enclosing composite action") differ
  from GitHub's and commitish's measured `$/` behaviour. Needs a
  measurement on a Gitea instance before `$/` typing is split per
  platform.
- Whether `actionslib`'s own lexer/parser (`pkg/internal/syntax`) still
  matches rhysd's grammar, or has drifted. Diff its testdata against
  act's.

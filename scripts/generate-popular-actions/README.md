# generate-popular-actions

This is a script for generating [`popular_actions.go`](../../popular_actions.go).

It does:

- Fetches metadata of popular actions
  - from https://github.com
  - from JSONL file in local
- Generates the fetched data set of metadata
  - as Go source file
  - as JSONL file

## Usage

Generate Go source:

```sh
go run ./scripts/generate-popular-actions ./popular_actions.go
```

Detect new releases on GitHub:

```sh
go run ./scripts/generate-popular-actions -d
```

Please see output of `-help` flag for more details.

## The data source file

The data source of the popular actions is defined in [`popular_actions.json`](./popular_actions.json). This file contains an array
of each action registry. Each registry is a JSON object containing the following keys:

| Key            | Description                                                     | Example                    | Required? |
| -------------- | --------------------------------------------------------------- | -------------------------- | --------- |
| `slug`         | GitHub repository slug                                          | `"actions/checkout"`       | Yes       |
| `tags`         | Known release tags                                              | `["v1", "v2", "v3", "v4"]` | Yes       |
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
Regenerate `popular_actions.go` with the command above after changing the registry.

Extraction preserves supported input fields when metadata also contains unknown
input keys, such as `type` or the misspelled `deprecation-message`. Extraction ignores
those keys. Invalid values, duplicate inputs and malformed YAML
still stop generation. This does not relax validation of local action metadata.

### References omitted from the October 2026 expansion

These supplied repository names were not publicly fetchable during verification:

- `actions-rs/fmt`
- `bitrise-io/bitrise-build`
- `cloudflare/pages-action`
- `codemagic-ci/codemagic-action`
- `hashicorp/setup-consul`
- `hashicorp/setup-vault`
- `microsoft/setup-msvc`
- `reviewdog/action-ansible-lint`
- `reviewdog/action-checkstyle`
- `reviewdog/action-clang-format`
- `reviewdog/action-clang-tidy`
- `reviewdog/action-dotenv-linter`
- `reviewdog/action-htmlhint`
- `reviewdog/action-phpcs`
- `reviewdog/action-phpstan`
- `reviewdog/action-prettier`
- `reviewdog/action-pylint`
- `reviewdog/action-scss-lint`
- `reviewdog/action-textlint`
- `reviewdog/action-vale`
- `step-security/wait-for-docker`

`netlify/actions/cli` had no `action.yml` or `action.yaml` at its default branch,
so there was no declared metadata to import. `gradle/actions` has a root action
that only reports an invalid-path error; the registry instead includes its
`setup-gradle`, `dependency-submission` and `wrapper-validation` sub-actions.

Legacy aliases with fetchable manifests, including `toolmantim/release-drafter`,
remain registered. Versions using retired runtimes enter
`OutdatedPopularActionSpecs` and receive the retired-runtime diagnostic.

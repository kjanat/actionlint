# Upstream tracker

This tracker lists [`rhysd/actionlint`] issues and pull requests that still need work in the fork, plus the evidence behind the contribution credits in [CHANGELOG.md].

## How to use this tracker

- Add each reviewed item once, under one disposition, with concrete evidence. Drop superseded, duplicate and out-of-scope items.
- A local issue or pull request created from a row carries the matching label: `upstream:still-affects`, `upstream:partial` or `upstream:candidate-to-port`. Change the label when the row moves.
- `Completed in` is the earliest kjanat tag that contains the behavior, or `Unreleased`.

## Still affects fork (5)

### Issues (5)

| Issue                | Title                                                                  | Author           | Evidence / notes                                                                                                                                 |
| -------------------- | ---------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| rhysd/actionlint#698 | Forgejo compatibility for `uses` lint                                  | @toonn           | No execution target validates Forgejo's absolute-URL `uses:` form.                                                                               |
| rhysd/actionlint#656 | Runner label checking with expressions is fragile                      | @rtarpine-vanta  | Runner resolution reads whole `matrix.x` references only. It does not fold interpolation inside matrix entries or concatenated `runs-on` values. |
| rhysd/actionlint#509 | actions/download-artifact false positives for GitHub Enterprise Server | @martincostello  | The `action-runtime` rule ignores a GHES `repository.service` and still flags artifact v3.                                                       |
| rhysd/actionlint#148 | Mix inherited with declared secrets                                    | @rethab          | A callee that declares `on.workflow_call.secrets` reports inherited organization secrets as undefined.                                           |
| rhysd/actionlint#110 | Single quote check is missed in presence of ${{ }} in if               | @varungandhi-src | A nested `${{ }}` inside `if:` gets only the always-true error. The string-literal checks on the comparison do not run.                          |

## Partially implemented (6)

### Pull requests (1)

| PR                   | Title              | Author     | Evidence / notes                     |
| -------------------- | ------------------ | ---------- | ------------------------------------ |
| rhysd/actionlint#366 | feat: Lint actions | @msw-kialo | The CLI lints `action.yml` directly. |

### Issues (5)

| Issue                | Title                                                          | Author              | Evidence / notes                                                                                                                                    |
| -------------------- | -------------------------------------------------------------- | ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| rhysd/actionlint#655 | No option to ignore default runners                            | @pratikkumar-mohite | `--runner-provider snapshot` checks only the snapshot's labels. Configuration cannot disable the built-in labels.                                   |
| rhysd/actionlint#525 | Rule suggestion: Explicit permissions for jobs                 | @jessehouwing       | `missing-permissions` cannot require an empty workflow baseline or enforce a job-count policy.                                                      |
| rhysd/actionlint#488 | Run jslint against steps using actions/github-script           | @jcgruenhage        | Script engines receive the `script` input of `actions/github-script` as JavaScript. No JavaScript linter plugin ships.                              |
| rhysd/actionlint#198 | Feature request: add option to enforce security best practices | @stefreak           | The `missing-permissions` rule and the opt-in `unpinned-reference` rule exist. Least-privilege enforcement remains open under rhysd/actionlint#525. |
| rhysd/actionlint#46  | Feature Request: Support linting action metadata files         | @mike-stewart       | The CLI lints `action.yml` directly.                                                                                                                |

## Candidate to port (16)

### Pull requests (1)

| PR                   | Title                                              | Author        | Evidence / notes                                                                                                                                            |
| -------------------- | -------------------------------------------------- | ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| rhysd/actionlint#706 | Support user-global config from `$XDG_CONFIG_HOME` | @janderssonse | It closes rhysd/actionlint#675. The fork has no user-global configuration. Upstream commit [`rhysd/actionlint@7633d7a`] needs design review before porting. |

### Issues (15)

| Issue                | Title                                                                                | Author         | Evidence / notes                                                                    |
| -------------------- | ------------------------------------------------------------------------------------ | -------------- | ----------------------------------------------------------------------------------- |
| rhysd/actionlint#675 | Feature request: global config from $XDG_CONFIG_HOME                                 | @janderssonse  | The fork has no user-global configuration. rhysd/actionlint#706 closes it.          |
| rhysd/actionlint#623 | Support configuration in `[tool.actionlint]` section of `pyproject.toml`             | @kdeldycke     | The fork reads no `pyproject.toml`.                                                 |
| rhysd/actionlint#564 | Add ability to advice replacing usually outdated *-latest runners with specific ones | @ssbarnea      | No rule warns against `*-latest` runners.                                           |
| rhysd/actionlint#538 | Detect deadlocks with concurrency groups                                             | @mschoettle    | No check compares concurrency groups across a caller and a local reusable workflow. |
| rhysd/actionlint#514 | Prevent attempts to use local actions in jobs before a repo is checked out           | @roryabraham   | No rule checks that a checkout precedes a local action.                             |
| rhysd/actionlint#501 | Allow enforcing all GHA expressions are wrapped in `${{}}`                           | @omus          | No opt-in rule requires `${{ }}` around `if:`.                                      |
| rhysd/actionlint#450 | Naming convention for inputs and outputs                                             | @nitrocode     | No rule checks input or output names.                                               |
| rhysd/actionlint#441 | Enforce \| before multiline run statement                                            | @roryabraham   | No diagnostic points at the missing block-scalar marker.                            |
| rhysd/actionlint#434 | Report redundant branch checks in GitHub Workflow jobs                               | @ilya-hontarau | No rule compares trigger branch filters with `if:` predicates.                      |
| rhysd/actionlint#417 | Detect malformed github variable declarations                                        | @karlbrown-va  | No diagnostic reports a spaced `$ {{ ... }}`.                                       |
| rhysd/actionlint#374 | Warning if explicit shell is not set                                                 | @jayvdb        | No opt-in rule requires an explicit `shell:`.                                       |
| rhysd/actionlint#362 | Add check for `too many workflows are referenced`                                    | @s0undt3ch     |                                                                                     |
| rhysd/actionlint#356 | Feature request: Check length of run statement.                                      | @joernott      |                                                                                     |
| rhysd/actionlint#246 | Feature request: Flag when the gh cli is used without providing GITHUB_TOKEN         | @roryabraham   | No check finds `gh` invocations without a token.                                    |
| rhysd/actionlint#69  | Feature request: redundant event names in expressions are errors                     | @webignition   | No rule compares literal `github.event_name` values with the trigger set.           |

## Needs reproduction (2)

### Issues (2)

| Issue                | Title                                                                                    | Author       | Evidence / notes                                                           |
| -------------------- | ---------------------------------------------------------------------------------------- | ------------ | -------------------------------------------------------------------------- |
| rhysd/actionlint#586 | actionlint missing findings reported by CodeQL on GitHub Actions workflow configurations | @aep-sunlife | The issue gives no workflow, CodeQL query IDs or mismatched findings.      |
| rhysd/actionlint#499 | False positive with complex runs-on statement                                            | @to-s        | The posted label list lacks the configured `runner=64cpu-linux-x64` value. |

## Implemented locally (33)

### Pull requests (23)

| PR                   | Title                                                                                                     | Author                | Completed in | Credit evidence                                                                                               |
| -------------------- | --------------------------------------------------------------------------------------------------------- | --------------------- | ------------ | ------------------------------------------------------------------------------------------------------------- |
| rhysd/actionlint#727 | Treat a matrix scalar tagged !!str as a string                                                            | @hdimer               | v1.13.0      | The author co-wrote kjanat/actionlint#38 ([`kjanat/actionlint@017a834`]).                                     |
| rhysd/actionlint#724 | Define jobs.<job_id>.result in workflow_call outputs                                                      | @Eljees               | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#718 | Update the popular actions data set                                                                       | @kolyshkin            | v1.9.0       | [`kjanat/actionlint@1c3a18e`] contains every action spec from this PR.                                        |
| rhysd/actionlint#716 | Rewrite CLI with cobra, add native shell completion                                                       | @Freed-Wu             | v1.13.0      | kjanat/actionlint#45 implements the request without this PR's code.                                           |
| rhysd/actionlint#715 | fix: include "type" and "options" in workflow_dispatch input key error                                    | @Socialpranker        | v1.13.0      | The author co-wrote kjanat/actionlint#25 ([`kjanat/actionlint@45dacee`]).                                     |
| rhysd/actionlint#714 | Fixes 713: Support vulnerability-alert permission                                                         | @kalverra             | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#710 | feat: support windows-11-vs2026-arm runner                                                                | @TheAlphaEngineerCode | v1.8.0       | [`kjanat/actionlint@6bdb110`] implements it.                                                                  |
| rhysd/actionlint#708 | Accept '+' sign in exponent part of number literals                                                       | @semx                 | v1.13.0      | The author co-wrote kjanat/actionlint#29 ([`kjanat/actionlint@788765f`]).                                     |
| rhysd/actionlint#707 | fix: add documented job workflow context properties                                                       | @alexchen-sys         | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#703 | Allow repository secrets in mixed-trigger workflows                                                       | @Eljees               | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#695 | Add `parallel-steps` rule: check wait/cancel references and steps allowed inside `parallel`               | @devantler            | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#689 | Support copilot-request permission                                                                        | @v1v                  | v1.13.0      | The author co-wrote kjanat/actionlint#28 ([`kjanat/actionlint@31e4dd7`]).                                     |
| rhysd/actionlint#674 | feat: add support for 'code-quality' permission                                                           | @smlx                 | v1.9.0       | [`kjanat/actionlint@1c3a18e`] implements it.                                                                  |
| rhysd/actionlint#670 | detect caller/callee permission mismatch in reusable workflow calls                                       | @SeriousBug           | v1.13.0      | kjanat/actionlint#41 implements it.                                                                           |
| rhysd/actionlint#654 | concurrency: Add support for `queue` key                                                                  | @vvoland              | v1.8.0       | [`kjanat/actionlint@ce89f3d`] implements it.                                                                  |
| rhysd/actionlint#653 | Add OCI image source label to the Docker image                                                            | @nozomiishii          | v1.13.0      | The author co-wrote kjanat/actionlint#26 ([`kjanat/actionlint@68913e8`]).                                     |
| rhysd/actionlint#649 | add config-secrets option to allow-list secrets in workflows                                              | @tdeo                 | v1.13.0      | The author co-wrote kjanat/actionlint#42 ([`kjanat/actionlint@0daf0f3`]).                                     |
| rhysd/actionlint#581 | feat: Support configurable timeout-minutes on jobs                                                        | @TomNorth             | v1.13.0      | The author co-wrote kjanat/actionlint#44 ([`kjanat/actionlint@46ead60`]).                                     |
| rhysd/actionlint#556 | Optimistically match shellcheck errors to source                                                          | @dpsutton             | v1.11.0      | [`kjanat/actionlint@660e090`] maps ShellCheck findings to source positions with new code and credits this PR. |
| rhysd/actionlint#482 | Add `shellcheck` dependency in pre-commit hooks                                                           | @kadykov              | v1.13.0      | The author co-wrote kjanat/actionlint#31 ([`kjanat/actionlint@b6ba05b`]).                                     |
| rhysd/actionlint#474 | Add required actions rule (and supporting tests) to enforce usage of specific GitHub Actions in workflows | @chrisreddington      | v1.13.0      | kjanat/actionlint#43 credits the idea and claims no code co-authorship.                                       |
| rhysd/actionlint#436 | Add optional lint to require that actions are pinned to commit hashes                                     | @mortenson            | v1.13.0      | The author co-wrote kjanat/actionlint#40 ([`kjanat/actionlint@ee2cb3f`]).                                     |
| rhysd/actionlint#332 | Add new script injection input                                                                            | @hugo-syn             | v1.13.0      | kjanat/actionlint#37 implements it.                                                                           |

### Issues (10)

| Issue                | Title                                                                                     | Author                   | Completed in | Credit evidence                                                      |
| -------------------- | ----------------------------------------------------------------------------------------- | ------------------------ | ------------ | -------------------------------------------------------------------- |
| rhysd/actionlint#682 | Add support for `ubuntu-26.04`                                                            | @ericcornelissen         | v1.8.0       | The changelog credits @ericcornelissen through rhysd/actionlint#683. |
| rhysd/actionlint#634 | Check that values in `exclude:` appear just in `matrix:`                                  | @muzimuzhi               | v1.16.0      | The changelog credits the report.                                    |
| rhysd/actionlint#552 | Actionlint should check permissions passed to reusable workflows (in the same repository) | @ianlewis                | v1.13.0      | kjanat/actionlint#41 implements it.                                  |
| rhysd/actionlint#524 | feature to require exact version number on a 3rd party action                             | @perryd01                | v1.13.0      | kjanat/actionlint#40 implements it.                                  |
| rhysd/actionlint#469 | Check whether a specific action has been used                                             | @chrisreddington         | v1.13.0      | kjanat/actionlint#43 credits the idea.                               |
| rhysd/actionlint#435 | Detect actions that are pinned to master/main                                             | @jmeickle-theaiinstitute | v1.13.0      | kjanat/actionlint#40 implements it.                                  |
| rhysd/actionlint#345 | [feature] Add new script injection input                                                  | @hugo-syn                | v1.13.0      | kjanat/actionlint#37 implements it.                                  |
| rhysd/actionlint#300 | [feature] shell completion                                                                | @Freed-Wu                | v1.13.0      | kjanat/actionlint#45 implements it.                                  |
| rhysd/actionlint#277 | Feature requests: Detect unsupported use of `timeout-minutes` in composite actions        | @stefreak                | v1.13.0      | kjanat/actionlint#70 implements it.                                  |
| rhysd/actionlint#250 | numeric string incorrectly considered a number in a property access                       | @altendky                | v1.13.0      | kjanat/actionlint#38 implements it.                                  |

## Archived upstream items (2)

| PR                   | Title                                                  | Author | Upstream state              | Completed in | Credit evidence                                                                   |
| -------------------- | ------------------------------------------------------ | ------ | --------------------------- | ------------ | --------------------------------------------------------------------------------- |
| rhysd/actionlint#257 | Make Actionlint Directly Invokable as an Action Itself | @norwd | Closed unmerged, 2023-01-18 | v1.11.0      | [`kjanat/actionlint@c41a8df`] credits @norwd as co-author of kjanat/actionlint#5. |
| rhysd/actionlint#479 | Make Actionlint Directly Invokable as an Action Itself | @norwd | Closed unmerged, 2024-11-19 | v1.11.0      | kjanat/actionlint#5 follows up on this proposal too.                              |

## Refresh procedure

1. Re-query all open issues and PRs with `gh issue list --repo rhysd/actionlint --state open --limit 1000` and `gh pr list --repo rhysd/actionlint --state open --limit 1000`.
2. Check items that left the open lists with `gh issue view` or `gh pr view`. Move credited ones to the archived section and drop the rest.
3. Update the row counts in the section headings.

<!-- markdownlint-disable-file no-duplicate-heading -->

[CHANGELOG.md]: CHANGELOG.md
[`rhysd/actionlint`]: https://github.com/rhysd/actionlint
[`kjanat/actionlint@017a834`]: https://github.com/kjanat/actionlint/commit/017a83492363ffa96da6a26f561a98c4b03efbc4
[`kjanat/actionlint@0daf0f3`]: https://github.com/kjanat/actionlint/commit/0daf0f3e978942c779fae9a2b461696d21217a0e
[`kjanat/actionlint@1c3a18e`]: https://github.com/kjanat/actionlint/commit/1c3a18e826166b8d88aa2ba25c5e7f73f420f57f
[`kjanat/actionlint@31e4dd7`]: https://github.com/kjanat/actionlint/commit/31e4dd777f8dfe14ca937ee605e914bdb7baa4fe
[`kjanat/actionlint@45dacee`]: https://github.com/kjanat/actionlint/commit/45daceeff077acfa346f62c76f14db3b1360cbb7
[`kjanat/actionlint@46ead60`]: https://github.com/kjanat/actionlint/commit/46ead60d28df37fc9d29cda7f2fefdb7a2b21d60
[`kjanat/actionlint@660e090`]: https://github.com/kjanat/actionlint/commit/660e09026c0fa2c7dba89925c54c5d5de85493b9
[`kjanat/actionlint@68913e8`]: https://github.com/kjanat/actionlint/commit/68913e875cc27f8a601c60f5fd79b581365f2466
[`kjanat/actionlint@6bdb110`]: https://github.com/kjanat/actionlint/commit/6bdb11050c8929e3a2209d79913f87bf0ba51e01
[`kjanat/actionlint@788765f`]: https://github.com/kjanat/actionlint/commit/788765f0ff09c16c0ce14e333417bbf6d0a9a5d2
[`kjanat/actionlint@b6ba05b`]: https://github.com/kjanat/actionlint/commit/b6ba05bbff1bfed7bc166f975a2c76cb2698dea4
[`kjanat/actionlint@c41a8df`]: https://github.com/kjanat/actionlint/commit/c41a8dffd6a5e7c9831156cee8c88cc4fcdf910f
[`kjanat/actionlint@ce89f3d`]: https://github.com/kjanat/actionlint/commit/ce89f3d74e18f88ba08774d2f85e1ff15fa173c9
[`kjanat/actionlint@ee2cb3f`]: https://github.com/kjanat/actionlint/commit/ee2cb3f7f4e7b9959285fee34bafcd361801c530
[`rhysd/actionlint@7633d7a`]: https://github.com/rhysd/actionlint/commit/7633d7a5d1e13bd39e665cfff74be8616f6692a3

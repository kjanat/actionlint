#!/usr/bin/env bash
set -euo pipefail

script=$(cd "$(dirname "$0")" && pwd)/autofix-step.bash
inputs_script=$(cd "$(dirname "$0")" && pwd)/autofix-inputs.bash
temp_root=$(cd "${TMPDIR:-/tmp}" && pwd -P)
temporary=$(mktemp -d "${temp_root}/actionlint-autofix-test.XXXXXX")
cleanup() {
	cd "${temp_root}"
	case "${temporary}" in
		"${temp_root}"/actionlint-autofix-test.*) rm -rf -- "${temporary}" ;;
		*) return 1 ;;
	esac
}
trap cleanup EXIT
mkdir "${temporary}/snapshots"
export TMPDIR="${temporary}/snapshots" GITHUB_OUTPUT="${temporary}/output"

git init --quiet "${temporary}/repo"
cd "${temporary}/repo"
git config core.autocrlf false
git config core.hooksPath "${temporary}/hooks"
echo original >tracked
echo ignored >.gitignore
git add --all
git -c user.name=Test -c user.email=test@example.com -c commit.gpgsign=false commit --quiet -m fixture

expect_change() {
	local expected=$1
	local output leftover
	shift
	: >"${GITHUB_OUTPUT}"
	bash "${script}" "$@"
	read -r output <"${GITHUB_OUTPUT}"
	[[ "${output}" == "changed=${expected}" ]]
	leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
	[[ -z "${leftover}" ]]
}

expect_change false true
expect_change true bash -c 'echo modernized > tracked'
expect_change false true
# Formatting the same file must still count as a separate change.
expect_change true bash -c 'echo formatted > tracked'
expect_change true bash -c 'echo generated > new-file'
expect_change true rm new-file
expect_change false bash -c 'echo cache > ignored'
expect_change false chmod +x tracked

git add tracked
echo unstaged >tracked
index_before=$(git hash-object .git/index)
expect_change false true
expect_change true bash -c 'echo updated > tracked'
index_after=$(git hash-object .git/index)
[[ "${index_after}" == "${index_before}" ]]

: >"${GITHUB_OUTPUT}"
status=0
bash "${script}" bash -c 'echo partial > tracked; exit 7' || status=$?
[[ "${status}" == 7 ]]
[[ ! -s "${GITHUB_OUTPUT}" ]]
leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
[[ -z "${leftover}" ]]

expect_inputs() {
	local base=$1 head=$2 tables=$3 readme=$4 nix=${5:-false}
	local output
	: >"${GITHUB_OUTPUT}"
	bash "${inputs_script}" "${base}" "${head}"
	output=$(cat "${GITHUB_OUTPUT}")
	[[ "${output}" == "$(printf 'tables=%s\nreadme=%s\nnix=%s' "${tables}" "${readme}" "${nix}")" ]]
}

check_input() {
	local path=$1 tables=$2 readme=$3 nix=${4:-false}
	local base head directory
	base=$(git write-tree)
	directory=$(dirname "${path}")
	mkdir -p "${directory}"
	echo fixture >"${path}"
	git add --all
	head=$(git write-tree)
	expect_inputs "${base}" "${head}" "${tables}" "${readme}" "${nix}"
	expect_inputs "${head}" "${base}" "${tables}" "${readme}" "${nix}"
}

check_input docs/unrelated.md false false
check_input internal/input/references/actions/remote/remote.go true true true
check_input internal/development/generators/action-metadata/main.go true true true
check_input internal/development/generators/event-triggers/main.go true true true
check_input tools/generate/webhook-payloads/main.mts true false
check_input internal/model/contexts/events/payload_catalogue.json true true
check_input package.json true false
check_input bun.lock true false
check_input internal/development/generators/expression-contexts/main.go true true true
check_input internal/model/contexts/availability.gen.go true true true
check_input internal/model/actionmeta/availability.gen.go true true true
check_input internal/model/actionmeta/runtimes.gen.go true true true
check_input internal/checking/rules/correctness/events.go true true true
check_input internal/checking/executor/expressions/check.go true true true
check_input popular_actions.go false false true
check_input internal/checking/rule_index.go true true true
check_input internal/checking/rule_registry.go true true true
check_input docs/rules.md true false
check_input internal/development/generators/rule-docs/main.go true true true
check_input internal/development/generators/action-metadata/runtimes.go true true true
check_input internal/model/actionmeta/metadata.go true true true
check_input cmd/actionlint/main.go false true true
check_input internal/languages/expressions/typecheck/check.go false true true
check_input mise.lock false true
check_input docs/screenshots/demo-workflow.yaml false false
check_input docs/screenshots/actionlint.yaml false false
check_input tools/docs/readme-demo.mjs false true
check_input README.md false true
check_input go.mod true true true
check_input go.sum true true true
check_input .github/workflows/autofix.yml true true true
check_input tools/ci/autofix-inputs.bash true true true
check_input flake.nix false false true
check_input flake.lock false false true
check_input distribution/nix/package.nix false false true
check_input tools/ci/update-nix-vendor-hash.bash false false true
check_input plugins/sdk/go/plugin.go false false false
check_input plugins/examples/insecure-download/main.go false false false
tree=$(git write-tree)
expect_inputs "${tree}" "${tree}" false false

status=0
bash "${inputs_script}" missing-ref HEAD 2>"${temporary}/error" || status=$?
[[ "${status}" != 0 ]]
: >"${GITHUB_OUTPUT}"

git symbolic-ref HEAD refs/heads/unborn
status=0
bash "${script}" touch should-not-exist 2>"${temporary}/error" || status=$?
[[ "${status}" != 0 ]]
[[ ! -e should-not-exist && ! -s "${GITHUB_OUTPUT}" ]]
leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
[[ -z "${leftover}" ]]
echo 'Autofix change tracking passed.'

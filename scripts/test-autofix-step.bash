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

# Preserve both staged and unstaged changes in the caller's index.
git add tracked
echo unstaged >tracked
index_before=$(git hash-object .git/index)
expect_change false true
expect_change true bash -c 'echo updated > tracked'
index_after=$(git hash-object .git/index)
[[ "${index_after}" == "${index_before}" ]]

# A failed tool must fail the step without publishing a success output.
: >"${GITHUB_OUTPUT}"
status=0
bash "${script}" bash -c 'echo partial > tracked; exit 7' || status=$?
[[ "${status}" == 7 ]]
[[ ! -s "${GITHUB_OUTPUT}" ]]
leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
[[ -z "${leftover}" ]]

# Exercise selection against real Git trees, including added and removed inputs.
expect_inputs() {
	local base=$1 head=$2 tables=$3 readme=$4
	local output
	: >"${GITHUB_OUTPUT}"
	bash "${inputs_script}" "${base}" "${head}"
	output=$(cat "${GITHUB_OUTPUT}")
	[[ "${output}" == "$(printf 'tables=%s\nreadme=%s' "${tables}" "${readme}")" ]]
}

check_input() {
	local path=$1 tables=$2 readme=$3
	local base head directory
	base=$(git write-tree)
	directory=$(dirname "${path}")
	mkdir -p "${directory}"
	echo fixture >"${path}"
	git add --all
	head=$(git write-tree)
	expect_inputs "${base}" "${head}" "${tables}" "${readme}"
	expect_inputs "${head}" "${base}" "${tables}" "${readme}"
}

check_input docs/unrelated.md false false
check_input scripts/generate-popular-actions/popular_actions.json true false
check_input scripts/generate-action-metadata/main.go true false
check_input scripts/generate-webhook-events/main.go true false
check_input scripts/generate-availability/main.go true false
check_input popular_actions.go true false
check_input docs/screenshots/demo-workflow.yaml false true
check_input docs/screenshots/actionlint.yaml false true
check_input scripts/check-readme/main.go false true
check_input README.md false true
check_input go.mod true true
check_input .github/workflows/autofix.yml true true
check_input scripts/autofix-inputs.bash true true
tree=$(git write-tree)
expect_inputs "${tree}" "${tree}" false false

# An unavailable comparison must fail the job.
status=0
bash "${inputs_script}" missing-ref HEAD 2>"${temporary}/error" || status=$?
[[ "${status}" != 0 ]]
: >"${GITHUB_OUTPUT}"

# A snapshot failure must stop before running the tool.
git symbolic-ref HEAD refs/heads/unborn
status=0
bash "${script}" touch should-not-exist 2>"${temporary}/error" || status=$?
[[ "${status}" != 0 ]]
[[ ! -e should-not-exist && ! -s "${GITHUB_OUTPUT}" ]]
leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
[[ -z "${leftover}" ]]
echo 'Autofix change tracking passed.'

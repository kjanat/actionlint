#!/usr/bin/env bash
set -euo pipefail

script=$(cd "$(dirname "$0")" && pwd)/autofix-step.bash
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

# A snapshot failure must stop before running the tool.
git symbolic-ref HEAD refs/heads/unborn
status=0
bash "${script}" touch should-not-exist 2>"${temporary}/error" || status=$?
[[ "${status}" != 0 ]]
[[ ! -e should-not-exist && ! -s "${GITHUB_OUTPUT}" ]]
leftover=$(find "${TMPDIR}" -mindepth 1 -print -quit)
[[ -z "${leftover}" ]]
echo 'Autofix change tracking passed.'

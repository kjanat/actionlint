#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "Usage: $0 IMAGE [PLATFORM]" >&2
	exit 2
fi

image="$1"
platform="${2:-linux/amd64}"
workspace="$(pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT
mkdir -p "${temporary}/workspace/.git" "${temporary}/commands"

output() {
	awk -v name="$1" '
        index($0, name "<<") == 1 {
            delimiter = substr($0, length(name) + 3)
            while ((getline line) > 0 && line != delimiter) {
                if (seen) printf "\n"
                printf "%s", line
                seen = 1
            }
            exit
        }
    ' "${temporary}/commands/output"
}

expect_output() {
	local actual
	actual="$(output "$1")"
	if [[ "${actual}" != "$2" ]]; then
		printf 'Expected %s=%s, got %s\n' "$1" "$2" "${actual}" >&2
		cat "${temporary}/action.log" >&2
		exit 1
	fi
}

expect_status() {
	local expected="$1" status=0
	shift
	: >"${temporary}/commands/output"
	docker run --rm --platform "${platform}" \
		--mount "type=bind,source=${temporary}/workspace,target=/github/workspace" \
		--mount "type=bind,source=${temporary}/commands,target=/github/file_commands" \
		--mount "type=bind,source=${workspace}/testdata,target=/github/workspace/testdata,readonly" \
		--workdir /github/workspace \
		-e GITHUB_ACTIONS=true -e GITHUB_WORKSPACE=/github/workspace \
		-e GITHUB_OUTPUT=/github/file_commands/output \
		"${image}" "$@" >"${temporary}/action.log" 2>&1 || status=$?
	if [[ "${status}" != "${expected}" ]]; then
		printf 'Expected exit %s, got %s\n' "${expected}" "${status}" >&2
		cat "${temporary}/action.log" >&2
		exit 1
	fi
}

expect_status 0 testdata/ok/minimal.yaml json '' '' true true . '' true
expect_output exit-code 0
expect_output result success
expect_output problems-found false
expect_output problem-count 0
output output | jq -e '.schema_version == 1 and .completed and .status == "success" and .diagnostics == []' >/dev/null
expect_output output-file ''

for format in github default oneline json json-lines markdown sarif; do
	expect_status 0 testdata/err/one_error.yaml "${format}" '' '' true true . '' false
	expect_output exit-code 1
	expect_output result problems-found
	expect_output problems-found true
	expect_output problem-count 1
done

expect_status 1 testdata/err/one_error.yaml github '' '' true true . '' true
expect_status 0 $'testdata/ok/minimal.yaml\ntestdata/err/one_error.yaml' json '' '' false false . '' false
expect_output problem-count 1
expect_status 0 testdata/err/one_error.yaml json '.*' '' false false . '' true
expect_output problem-count 0

for tool in shellcheck pyflakes; do
	case "${tool}" in
		shellcheck)
			fixture=testdata/err/shellcheck_default_shell_detection.yaml
			shellcheck=true
			pyflakes=false
			;;
		pyflakes)
			fixture=testdata/err/pyflakes_step_shell.yaml
			shellcheck=false
			pyflakes=true
			;;
		*) exit 1 ;;
	esac
	expect_status 0 "${fixture}" json '' '' "${shellcheck}" "${pyflakes}" . '' false
	problem_count="$(output problem-count)"
	[[ "${problem_count}" -gt 0 ]]
	expect_status 0 "${fixture}" json '' '' false false . '' true
	expect_output problem-count 0
done

mkdir -p "${temporary}/workspace/sub/.github/workflows"
cp testdata/ok/minimal.yaml "${temporary}/workspace/sub/.github/workflows/check.yaml"
printf 'self-hosted-runner:\n  labels: [custom-runner]\n' >"${temporary}/workspace/sub/lint.yaml"
expect_status 0 '.github/workflows/check.yaml' json '' lint.yaml false false sub reports/result.json true
expect_output output-file reports/result.json
report="$(cat "${temporary}/workspace/reports/result.json")"
test "${report}" = '[]'
owner="$(stat -c '%u:%g' "${temporary}/workspace")"
directory_owner="$(stat -c '%u:%g' "${temporary}/workspace/reports")"
report_owner="$(stat -c '%u:%g' "${temporary}/workspace/reports/result.json")"
test "${directory_owner}" = "${owner}"
test "${report_owner}" = "${owner}"

expect_status 2 '' invalid '' '' true true . '' true
expect_status 2 '' '' '' '' true true . '' true
expect_status 2 '' json '' '' '' true . '' true
expect_status 2 '' json '' '' true '' . '' true
expect_status 2 '' json '' '' true true . '' ''
expect_status 2 --help json '' '' true true . '' true
expect_status 2
expect_status 3 missing.yaml json '' '' false false . '' true

echo 'Docker Action compatibility passed'

#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]]; then
	echo "Usage: $0 IMAGE" >&2
	exit 2
fi

image="$1"
workspace="$(pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

mkdir -p "${tmp}/file-commands" "${tmp}/workspace/testdata"

function output() {
	local name="$1"
	awk -v name="${name}" '
        index($0, name "<<") == 1 {
            delimiter = substr($0, length(name) + 3)
            while ((getline line) > 0 && line != delimiter) {
                if (seen) {
                    printf "\n"
                }
                printf "%s", line
                seen = 1
            }
            exit
        }
    ' "${tmp}/file-commands/output"
}

function run_action() {
	: >"${tmp}/file-commands/output"
	: >"${tmp}/action.log"
	docker run --rm \
		--mount "type=bind,source=${tmp}/workspace,target=/github/workspace" \
		--mount "type=bind,source=${workspace}/testdata,target=/github/workspace/testdata,readonly" \
		--mount "type=bind,source=${tmp}/file-commands,target=/github/file_commands" \
		--workdir /github/workspace \
		-e GITHUB_ACTIONS=true \
		-e GITHUB_OUTPUT=/github/file_commands/output \
		-e GITHUB_WORKSPACE=/github/workspace \
		"$image" "$@" >"${tmp}/action.log" 2>&1
}

function show_log() {
	sed 's/^/action test: /' "${tmp}/action.log" >&2
}

function expect_status() {
	local expected="$1"
	local description="$2"
	shift 2
	if run_action "$@"; then
		echo "Expected ${description} to fail with status ${expected}" >&2
		show_log
		exit 1
	else
		local status="$?"
	fi
	if [[ "${status}" != "${expected}" ]]; then
		echo "Expected ${description} status ${expected}, got ${status}" >&2
		show_log
		exit 1
	fi
}

if ! run_action testdata/ok/minimal.yaml json '' '' true true . '' true; then
	show_log
	exit 1
fi
test "$(output exit-code)" = 0
test "$(output result)" = success
test "$(output problems-found)" = false
test "$(output problem-count)" = 0
test "$(output output)" = '[]'

for format in github default oneline json json-lines markdown sarif; do
	if ! run_action testdata/err/one_error.yaml "${format}" '' '' true true . '' false; then
		show_log
		exit 1
	fi
	test "$(output exit-code)" = 1
	test "$(output result)" = problems-found
	test "$(output problems-found)" = true
	test "$(output problem-count)" = 1
done

run_action testdata/err/one_error.yaml json-lines '' '' true true . actionlint-results.jsonl false
test "$(output output-file)" = actionlint-results.jsonl
grep -q '"message"' "${tmp}/workspace/actionlint-results.jsonl"

expect_status 1 'actionlint findings' testdata/err/one_error.yaml github '' '' true true . '' true
test "$(output exit-code)" = 1
test "$(output result)" = problems-found

expect_status 2 'an invalid format' '' invalid '' '' true true . '' true
expect_status 2 'an escaping working-directory' testdata/ok/minimal.yaml json '' '' true true .. '' true
expect_status 2 'an escaping output-file' testdata/ok/minimal.yaml json '' '' true true . ../escaped.json true
test ! -e "${tmp}/escaped.json"
expect_status 2 'a directory output-file' testdata/ok/minimal.yaml json '' '' true true . . true
expect_status 2 'an escaping config-file' testdata/ok/minimal.yaml json '' ../actionlint.yaml true true . '' true
expect_status 2 'an option-like file path' --help json '' '' true true . '' true

echo 'GitHub Action image tests passed'

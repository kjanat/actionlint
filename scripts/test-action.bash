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

mkdir -p \
	"${tmp}/github/file_commands" \
	"${tmp}/github/workspace/.git" \
	"${tmp}/github/workspace/.github/workflows" \
	"${tmp}/github/workspace/testdata"

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
    ' "${tmp}/github/file_commands/output"
}

function reset_action_files() {
	: >"${tmp}/github/file_commands/output"
	: >"${tmp}/action.log"
}

function docker_action() {
	local status=0
	docker run --rm \
		--mount "type=bind,source=${tmp}/github,target=/github" \
		--mount "type=bind,source=${workspace}/testdata,target=/github/workspace/testdata,readonly" \
		--workdir /github/workspace \
		-e GITHUB_ACTIONS=true \
		-e GITHUB_OUTPUT=/github/file_commands/output \
		-e GITHUB_WORKSPACE=/github/workspace \
		"${image}" "$@" >"${tmp}/action.log" 2>&1 || status="$?"
	echo "${status}"
}

function run_action() {
	local status
	reset_action_files
	status="$(docker_action "$@")"
	return "${status}"
}

function show_log() {
	sed 's/^/action test: /' "${tmp}/action.log" >&2
}

function assert_output() {
	local name="$1"
	local expected="$2"
	local actual
	actual="$(output "${name}")"
	if [[ "${actual}" != "${expected}" ]]; then
		echo "Expected output '${name}' to be '${expected}', got '${actual}'" >&2
		show_log
		exit 1
	fi
}

function assert_log_matches() {
	local expected="$1"
	if ! grep -Eq "${expected}" "${tmp}/action.log"; then
		echo "Expected action log to match '${expected}'" >&2
		show_log
		exit 1
	fi
}

function action_status() {
	reset_action_files
	docker_action "$@"
}

function expect_status() {
	local expected="$1"
	local description="$2"
	shift 2
	local status
	status="$(action_status "$@")"
	if [[ "${status}" == 0 ]]; then
		echo "Expected ${description} to fail with status ${expected}" >&2
		show_log
		exit 1
	fi
	if [[ "${status}" != "${expected}" ]]; then
		echo "Expected ${description} status ${expected}, got ${status}" >&2
		show_log
		exit 1
	fi
}

function expect_success() {
	local status
	status="$(action_status "$@")"
	if [[ "${status}" != 0 ]]; then
		show_log
		exit 1
	fi
}

expect_success testdata/ok/minimal.yaml json '' '' true true . '' true
assert_log_matches '^actionlint [^:]+: 0 problems in 1 workflow file \(shellcheck, pyflakes\)$'
assert_output exit-code 0
assert_output result success
assert_output problems-found false
assert_output problem-count 0
assert_output output '[]'

for format in github default oneline json json-lines markdown sarif; do
	expect_success testdata/err/one_error.yaml "${format}" '' '' true true . '' false
	assert_log_matches '^actionlint [^:]+: 1 problem in 1 workflow file \(shellcheck, pyflakes\)$'
	assert_output exit-code 1
	assert_output result problems-found
	assert_output problems-found true
	assert_output problem-count 1
done

expect_status 3 'an empty workflow directory' '' github '' '' true true . '' true
assert_log_matches '^actionlint [^:]+: failed with unknown problems while checking 0 workflow files \(shellcheck, pyflakes\)$'

run_action testdata/err/shellcheck_default_shell_detection.yaml oneline '' '' true false . '' false
assert_output problem-count 12

run_action testdata/err/shellcheck_default_shell_detection.yaml oneline '' '' false false . '' false
assert_output problem-count 0

run_action testdata/err/pyflakes_step_shell.yaml oneline '' '' false true . '' false
assert_output problem-count 3

run_action testdata/err/pyflakes_step_shell.yaml oneline '' '' false false . '' false
assert_output problem-count 0

run_action testdata/err/one_error.yaml json-lines '' '' true true . actionlint-results.jsonl false
assert_output output-file actionlint-results.jsonl
grep -q '"message"' "${tmp}/github/workspace/actionlint-results.jsonl"

expect_status 1 'actionlint findings' testdata/err/one_error.yaml github '' '' true true . '' true
assert_output exit-code 1
assert_output result problems-found

expect_status 2 'an invalid format' '' invalid '' '' true true . '' true
expect_status 2 'an escaping working-directory' testdata/ok/minimal.yaml json '' '' true true .. '' true
expect_status 2 'an escaping output-file' testdata/ok/minimal.yaml json '' '' true true . ../escaped.json true
test ! -e "${tmp}/github/escaped.json"
expect_status 2 'a directory output-file' testdata/ok/minimal.yaml json '' '' true true . . true
expect_status 2 'an escaping config-file' testdata/ok/minimal.yaml json '' ../actionlint.yaml true true . '' true
expect_status 2 'an option-like file path' --help json '' '' true true . '' true

echo 'GitHub Action image tests passed'

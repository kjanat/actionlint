#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=.github/actions/setup-pandoc/retry.bash
source ./retry.bash

calls=0
delays=()
sleep() { delays+=("$1"); }
request() {
	((calls += 1))
	[[ "$1" == 'argument with spaces' && "$2" == '*' ]] || return 99
	if ((calls < succeed_on)); then
		return 23
	fi
	printf '{"tagName":"3.11"}\n'
}

succeed_on=1
retry request 'argument with spaces' '*' >/dev/null
[[ "${calls}" == 1 && "${#delays[@]}" == 0 ]]

calls=0
succeed_on=3
retry request 'argument with spaces' '*' >/dev/null
[[ "${calls}" == 3 && "${delays[*]}" == '2 4' ]]

calls=0
delays=()
succeed_on=5
set +e
retry request 'argument with spaces' '*'
status=$?
set -e
[[ "${status}" == 23 ]]
[[ "${calls}" == 4 && "${delays[*]}" == '2 4 8' ]]

calls=0
succeed_on=2
json="$(retry request 'argument with spaces' '*')"
[[ "${json}" == '{"tagName":"3.11"}' ]]
printf 'Pandoc retry tests passed\n'

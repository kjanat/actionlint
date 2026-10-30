#!/usr/bin/env bash
# Published Wasm plugins must work as the image's non-root user, including offline.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "Usage: $0 IMAGE [PLATFORM]" >&2
	exit 2
fi

image="$1"
platform="${2:-linux/amd64}"
volume="$(docker volume create)"
trap 'docker volume rm "${volume}" >/dev/null' EXIT

online="$(docker run --rm --platform "${platform}" \
	--mount "type=volume,source=${volume},target=/home/actionlint" \
	--entrypoint /bin/sh "${image}" -ec '
	test "$(id -u)" = 405
	test "${HOME}" = /home/actionlint
	test -w "${HOME}"
	mkdir "${HOME}/project"
	cd "${HOME}/project"
	git init --quiet
	actionlint plugin install shellcheck@v0.2.0 >&2
	actionlint plugin install ruff@v0.2.0 >&2
	cat > workflow.yml <<"YAML"
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $GITHUB_SHA
      - shell: python
        run: print(unknown_name)
YAML
	cp .github/actionlint.plugins.lock.json lock.before
	status=0
	actionlint --json workflow.yml || status=$?
	test "${status}" = 1
	cmp .github/actionlint.plugins.lock.json lock.before
')"
jq -e '.completed and .exit_code == 1
	and any(.diagnostics[]; .rule == "shellcheck" and .code == "SC2086")
	and any(.diagnostics[]; .rule == "ruff" and .code == "F821")' <<<"${online}" >/dev/null

offline="$(docker run --rm --platform "${platform}" --network none \
	--mount "type=volume,source=${volume},target=/home/actionlint" \
	--entrypoint /bin/sh "${image}" -ec '
	test "$(id -u)" = 405
	cd "${HOME}/project"
	status=0
	actionlint --json workflow.yml || status=$?
	test "${status}" = 1
	cmp .github/actionlint.plugins.lock.json lock.before
')"
online="$(jq -cS . <<<"${online}")"
offline="$(jq -cS . <<<"${offline}")"
if [[ "${online}" != "${offline}" ]]; then
	printf 'Offline results differ from the published-plugin run:\n%s\n%s\n' "${online}" "${offline}" >&2
	exit 1
fi

echo 'Non-root Docker Wasm plugin and offline cache checks passed.'

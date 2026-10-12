#!/usr/bin/env bash
# Verify the image (also used by actionlint-bundled) includes working Ruff.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "Usage: $0 IMAGE [PLATFORM]" >&2
	exit 2
fi

image="$1"
platform="${2:-linux/amd64}"
workspace="$(pwd)"

docker run --rm --platform "${platform}" --entrypoint /usr/local/bin/ruff "${image}" --version
status=0
diagnostics="$(docker run --rm --platform "${platform}" \
	--mount "type=bind,source=${workspace},target=/w,readonly" \
	--workdir /w --entrypoint /usr/local/bin/actionlint \
	"${image}" --no-config --shellcheck= --json nix/integration.yml)" || status=$?
if [[ "${status}" != 1 ]]; then
	printf 'Expected a Ruff finding (exit 1), got exit %s:\n%s\n' "${status}" "${diagnostics}" >&2
	exit 1
fi
jq -e 'any(.diagnostics[]; .rule == "ruff" and .code == "F821")' <<<"${diagnostics}"

docker run --rm --platform "${platform}" \
	--mount "type=bind,source=${workspace},target=/w,readonly" \
	--workdir /w --entrypoint /usr/local/bin/actionlint \
	"${image}" --no-config --shellcheck= --ruff= --json nix/integration.yml \
	| jq -e '.diagnostics == []'

echo 'Bundled Docker Ruff checks passed.'

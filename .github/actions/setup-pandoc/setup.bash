#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_ACTION_PATH:?}" "${RUNNER_TEMP:?}"

version=3.11
arch="$(dpkg --print-architecture)"
case "${arch}" in
	amd64 | arm64) ;;
	*)
		printf 'Unsupported Pandoc architecture: %s\n' "${arch}" >&2
		exit 1
		;;
esac
asset="pandoc-${version}-1-${arch}.deb"
digest="$(awk -v asset="${asset}" '$2 == asset { print $1 }' "${GITHUB_ACTION_PATH}/SHA256SUMS")"
[[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || {
	echo 'Missing Pandoc SHA-256 pin' >&2
	exit 1
}
cache_dir="${RUNNER_TEMP}/pandoc-cache/${digest}"
archive="${cache_dir}/${asset}"

case "${1:-}" in
	prepare)
		: "${GITHUB_OUTPUT:?}"
		printf 'cache-dir=%s\ncache-key=pandoc-deb-v1-%s-%s\n' "${cache_dir}" "${arch}" "${digest}" >>"${GITHUB_OUTPUT}"
		;;
	install)
		mkdir -p "${cache_dir}"
		trap 'rm -f -- "${archive}.tmp"' EXIT
		if [[ ! -f "${archive}" ]]; then
			# Public, immutable release bytes: no authenticated API discovery, even on a cold cache.
			curl --fail --location --retry 3 --proto '=https' --proto-redir '=https' \
				"https://github.com/jgm/pandoc/releases/download/${version}/${asset}" --output "${archive}.tmp"
			printf '%s  %s\n' "${digest}" "${archive}.tmp" | sha256sum --check --status
			mv "${archive}.tmp" "${archive}"
		fi
		# A cache hit is untrusted until its bytes match the reviewed pin, too.
		printf '%s  %s\n' "${digest}" "${archive}" | sha256sum --check --status
		sudo dpkg --install "${archive}"
		;;
	*)
		echo 'Expected prepare or install' >&2
		exit 1
		;;
esac

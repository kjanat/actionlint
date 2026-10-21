#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_ACTION_PATH:?}" "${RUNNER_TEMP:?}"
[[ "${RUNNER_OS:-}" == Linux && "${RUNNER_ARCH:-}" == X64 ]] || {
	echo 'Pinned formatters require Linux/X64' >&2
	exit 1
}
pins="${GITHUB_ACTION_PATH}/pins.tsv"
identity="$(sha256sum "${pins}")"
identity="${identity%% *}"
cache_dir="${RUNNER_TEMP}/formatter-cache/${identity}"
bin_dir="${RUNNER_TEMP}/formatter-bin/${identity}"

case "${1:-}" in
	prepare)
		: "${GITHUB_OUTPUT:?}"
		printf 'cache-dir=%s\ncache-key=formatters-linux-x64-v1-%s\n' "${cache_dir}" "${identity}" >>"${GITHUB_OUTPUT}"
		;;
	install)
		: "${GITHUB_PATH:?}"
		mkdir -p "${cache_dir}" "${bin_dir}"
		staging="$(mktemp -d "${RUNNER_TEMP}/formatters.XXXXXX")"
		trap 'rm -rf -- "${staging}"' EXIT
		while IFS=$'\t' read -r name version repo tag asset digest member; do
			[[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || {
				echo 'Invalid formatter SHA-256 pin' >&2
				exit 1
			}
			archive="${cache_dir}/${asset}"
			if [[ ! -f "${archive}" ]]; then
				curl --fail --location --retry 3 --connect-timeout 15 --max-time 120 --proto '=https' --proto-redir '=https' \
					"https://github.com/${repo}/releases/download/${tag}/${asset}" --output "${staging}/${asset}"
				printf '%s  %s\n' "${digest}" "${staging}/${asset}" | sha256sum --check --status
				mv "${staging}/${asset}" "${archive}"
			fi
			# Cached archives and existing executables are never trusted without verification.
			printf '%s  %s\n' "${digest}" "${archive}" | sha256sum --check --status
			case "${asset}" in
				*.zip) unzip -p "${archive}" "${member}" >"${staging}/${name}" ;;
				*.tar.gz) tar -xOzf "${archive}" "${member}" >"${staging}/${name}" ;;
				*)
					echo 'Unsupported formatter archive' >&2
					exit 1
					;;
			esac
			chmod +x "${staging}/${name}"
			"${staging}/${name}" --version | grep -F -- "${version}"
		done <"${pins}"
		for name in dprint tombi; do
			mv -f "${staging}/${name}" "${bin_dir}/${name}"
		done
		printf '%s\n' "${bin_dir}" >>"${GITHUB_PATH}"
		;;
	*)
		echo 'Expected prepare or install' >&2
		exit 1
		;;
esac

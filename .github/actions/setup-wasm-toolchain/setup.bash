#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_ACTION_PATH:?}" "${RUNNER_TEMP:?}" "${RUNNER_OS:?}" "${RUNNER_ARCH:?}"

if [[ "${RUNNER_OS}/${RUNNER_ARCH}" != Linux/X64 ]]; then
	echo 'The plugin fixture toolchain requires Linux/X64.' >&2
	exit 1
fi
pins="${GITHUB_ACTION_PATH}/releases.json"
jq -e 'type == "array" and length == 2 and ([.[].name] | sort == ["wasm-tools", "wit-bindgen"]) and all(.[]; (.version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) and (.sha256 | test("^[a-f0-9]{64}$")))' "${pins}" >/dev/null
identity="$(sha256sum "${pins}")"
cache_dir="${RUNNER_TEMP}/wasm-toolchain/${identity%% *}"

case "${1:-}" in
	prepare)
		: "${GITHUB_OUTPUT:?}"
		printf 'cache-dir=%s\ncache-key=wasm-toolchain-v1-linux-x64-%s\n' "${cache_dir}" "${identity%% *}" >>"${GITHUB_OUTPUT}"
		;;
	install)
		: "${GITHUB_PATH:?}"
		mkdir -p "${cache_dir}"
		install_dir="$(mktemp -d "${RUNNER_TEMP}/wasm-tools.XXXXXXXX")"
		releases="$(jq -r '.[] | [.name, .version, .sha256] | @tsv' "${pins}" | tr -d '\r')"
		while IFS=$'\t' read -r name version digest; do
			base="${name}-${version}-x86_64-linux"
			archive="${cache_dir}/${digest}.tar.gz"
			trap 'rm -f -- "${archive}.tmp"' EXIT
			if ! printf '%s  %s\n' "${digest}" "${archive}" | sha256sum --check --status 2>/dev/null; then
				curl --fail --location --silent --show-error --retry 3 --proto '=https' --proto-redir '=https' \
					"https://github.com/bytecodealliance/${name}/releases/download/v${version}/${base}.tar.gz" --output "${archive}.tmp"
				printf '%s  %s\n' "${digest}" "${archive}.tmp" | sha256sum --check --status
				mv "${archive}.tmp" "${archive}"
			fi
			tar -xzf "${archive}" -C "${install_dir}" --strip-components=1 "${base}/${name}"
			"${install_dir}/${name}" --version
		done <<<"${releases}"
		printf '%s\n' "${install_dir}" >>"${GITHUB_PATH}"
		;;
	*)
		echo 'Expected prepare or install' >&2
		exit 1
		;;
esac

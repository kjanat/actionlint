#!/usr/bin/env bash
set -euo pipefail

action_path=${GITHUB_ACTION_PATH:?}
runner_os=${RUNNER_OS:?}
runner_arch=${RUNNER_ARCH:?}
runner_temp=${RUNNER_TEMP:?}
update_path=${UPDATE_PATH:?}
gh_repo=${GH_REPO:?}
github_output=${GITHUB_OUTPUT:?}
github_path=${GITHUB_PATH:?}
version=${VERSION-}
tag=${TAG-}
cache_dir=${CACHE_DIR-}
release_file=${RELEASE_FILE-}

source "${action_path//\\//}/retry.bash"

case "${runner_os}/${runner_arch}" in
	Linux/X64) platform=linux.x86_64 ;;
	Linux/ARM64) platform=linux.aarch64 ;;
	macOS/X64) platform=darwin.x86_64 ;;
	macOS/ARM64) platform=darwin.aarch64 ;;
	# The Windows release uses x64 emulation on ARM64 runners.
	Windows/X64 | Windows/ARM64) platform=windows ;;
	*)
		echo '::error::Unsupported runner platform.'
		exit 1
		;;
esac
case "${update_path}" in
	true | false) ;;
	*)
		echo '::error::update-path must be true or false.'
		exit 1
		;;
esac

prepare() {
	local tag release_file
	release_file="$(mktemp "${runner_temp//\\//}/shellcheck-release.XXXXXXXX")"
	if [[ "${version}" == latest ]]; then
		retry gh api "repos/${gh_repo}/releases/latest" >"${release_file}"
		tag="$(jq -r '.tag_name' "${release_file}")"
	elif [[ "${version}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		tag="v${version#v}"
	else
		echo '::error::shellcheck-version must be a release version or latest.'
		exit 1
	fi
	if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		echo '::error::The release does not have a stable ShellCheck version tag.'
		exit 1
	fi
	printf 'tag=%s\nrelease-file=%s\ncache-dir=%s/actionlint-shellcheck/%s/%s\n' \
		"${tag}" "${release_file}" "${runner_temp//\\//}" "${tag}" "${platform}" >>"${github_output}"
}

checksum=(sha256sum)
if [[ "${runner_os}" == macOS ]]; then checksum=(shasum -a 256); fi

verified_archive() {
	local digest actual
	cache_valid=false
	if [[ -f "${cache_dir}/archive" && -f "${cache_dir}/sha256" ]]; then
		digest="$(cat "${cache_dir}/sha256")"
		if [[ "${digest}" =~ ^[0-9a-f]{64}$ ]]; then
			actual="$("${checksum[@]}" "${cache_dir}/archive")"
			if [[ "${actual%% *}" == "${digest}" ]]; then cache_valid=true; fi
		fi
	fi
}

install_shellcheck() {
	local release release_tag metadata asset digest archive actual install_dir binary=shellcheck
	local -a assets=("shellcheck-${tag}.${platform}.tar.gz" "shellcheck-${tag}.${platform}.tar.xz")
	if [[ "${platform}" == windows ]]; then
		assets=("shellcheck-${tag}.zip")
		binary=shellcheck.exe
	fi
	verified_archive
	if [[ "${cache_valid}" == false ]]; then
		if [[ -e "${cache_dir}/archive" || -e "${cache_dir}/sha256" ]]; then
			echo '::warning::Discarding corrupt ShellCheck cache; downloading a verified archive.'
		fi
		if [[ -s "${release_file}" ]]; then
			release="$(cat "${release_file}")"
		else
			release="$(retry gh api "repos/${gh_repo}/releases/tags/${tag}")"
		fi
		release_tag="$(jq -r '.tag_name' <<<"${release}")"
		if [[ "${release_tag}" != "${tag}" ]]; then
			echo '::error::The release does not match the requested ShellCheck version.'
			exit 1
		fi
		for asset in "${assets[@]}"; do
			metadata="$(jq -c --arg name "${asset}" '.assets[] | select(.name == $name)' <<<"${release}")"
			if [[ -n "${metadata}" ]]; then break; fi
		done
		if [[ -z "${metadata}" ]]; then
			echo "::error::No ShellCheck release asset is available for ${platform} in ${tag}."
			exit 1
		fi
		digest="$(jq -r '.digest' <<<"${metadata}")"
		if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
			echo "::error::The release asset ${asset} is missing a valid SHA-256 checksum."
			exit 1
		fi
		archive="$(mktemp "${runner_temp//\\//}/shellcheck-archive.XXXXXXXX")"
		# This is a public release: avoid gh release download's second metadata request.
		retry curl --fail --location --silent --show-error --output "${archive}" \
			"https://github.com/${gh_repo}/releases/download/${tag}/${asset}"
		actual="$("${checksum[@]}" "${archive}")"
		if [[ "${actual%% *}" != "${digest#sha256:}" ]]; then
			rm -f "${archive}"
			echo '::error::The ShellCheck download does not match its SHA-256 checksum.'
			exit 1
		fi
		mkdir -p "${cache_dir}"
		mv "${archive}" "${cache_dir}/archive"
		printf '%s\n' "${digest#sha256:}" >"${cache_dir}/sha256"
	fi
	install_dir="$(mktemp -d "${runner_temp//\\//}/shellcheck.XXXXXXXX")"
	if [[ "${platform}" == windows ]]; then
		unzip -p "${cache_dir}/archive" shellcheck.exe >"${install_dir}/${binary}"
		install_dir="$(cygpath -m "${install_dir}")"
	else
		tar -xf "${cache_dir}/archive" -C "${install_dir}" --strip-components=1 "shellcheck-${tag}/shellcheck"
	fi
	"${install_dir}/${binary}" --version
	printf 'shellcheck-path=%s/%s\n' "${install_dir}" "${binary}" >>"${github_output}"
	if [[ "${update_path}" == true ]]; then
		printf '%s\n' "${install_dir}" >>"${github_path}"
	fi
	rm -f "${release_file}"
}

case "$1" in
	prepare) prepare ;;
	install) install_shellcheck ;;
	*)
		echo '::error::Unknown ShellCheck setup phase.'
		exit 1
		;;
esac

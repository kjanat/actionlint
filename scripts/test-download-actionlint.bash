#!/usr/bin/env bash

set -o pipefail
set -e

if [[ ! -d .git ]]; then
	echo 'This script must be run from root of repository' >&2
	exit 1
fi

set -x

script="$(pwd)/scripts/download-actionlint.bash"
temp_dir="$(mktemp -d)"
trap 'popd && rm -rf $temp_dir' EXIT
pushd "${temp_dir}"

# Normal cases
set -e

check_latest_version() {
	local executable="$1"
	local out first_line version

	out="$("${executable}" -version)"
	first_line="${out%%$'\n'*}"
	version="${first_line#actionlint.kjanat.dev }"
	if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
		|| [[ "${out}" != *"https://github.com/kjanat/actionlint/releases/tag/v${version}"* ]]; then
		echo "Output from ${executable} -version is unexpected: '${out}'" >&2
		exit 1
	fi
}

# Latest release
github_output="${temp_dir}/github-output"
out="$(GITHUB_ACTION=true GITHUB_OUTPUT="${github_output}" bash "${script}" latest)"
if ! grep -Fqx "executable=${temp_dir}/actionlint" "${github_output}"; then
	echo "'executable' step output is not set correctly in ${github_output}:" >&2
	cat "${github_output}" >&2
	echo "Download script output: '${out}'" >&2
	exit 1
fi
check_latest_version ./actionlint
rm -f ./actionlint

# Specify only version
bash "${script}" '1.8.0'
out="$(./actionlint -version | head -n 1)"
if [[ "${out}" != '1.8.0' ]]; then
	echo "Unexpected version: '${out}'" 1>&2
	exit 1
fi
rm -f ./actionlint

# Specify only a download directory
mkdir ./test1
bash "${script}" latest ./test1
check_latest_version ./test1/actionlint
rm -rf ./test1

# Specify both version and a download directory
mkdir ./test2
bash "${script}" '1.8.0' ./test2
out="$(./test2/actionlint -version | head -n 1)"
if [[ "${out}" != '1.8.0' ]]; then
	echo "Unexpected version: '${out}'" 1>&2
	exit 1
fi
rm -rf ./test2

# Error cases
set +e

fails=0
if bash "${script}" 'v1.8.0'; then
	echo "FAIL: Invalid version at the first argument did not cause any error" >&2
	((fails++))
fi
if bash "${script}" './this/dir/does/not/exist'; then
	echo "FAIL: Directory which does not exist at the first argument did not cause any error" >&2
	((fails++))
fi
if bash "${script}" '999999999999999999.9.9'; then
	echo "FAIL: Unknown version at the first argument did not cause any error" >&2
	((fails++))
fi

set -e
if [[ "${fails}" != "0" ]]; then
	echo "${fails} error cases failed. Check the above log" >&2
	exit 1
fi

echo 'SUCCESS'

#!/usr/bin/env bash
# Usage: package.sh [component.wasm] [output-directory]
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
component="${1:-no-sudo.wasm}"
output="${2:-dist}"
contract="${ACTIONLINT_WIT:-../../protocol/wit}"
sdk="${WASI_SDK_PATH:-/opt/wasi-sdk}"
license="$(sed 's/\r$//' LICENSE)"

wasm-tools validate "${component}"
mkdir -p "${output}"
package="$(head -n 1 "${contract}/analyzer.wit" | tr -d '\r;')"
package="${package#package }"
interface="${package%@*}/analyzer@${package##*@}"
contract_hash="$(tr -d '\r' <"${contract}/analyzer.wit" | sha256sum)"
contract_hash="${contract_hash%% *}"

"${sdk}/bin/wasm32-wasip2-clang" -E -P -x c metadata.json.in \
	| jq --arg interface "${interface}" --arg contract "${contract_hash}" '
        .interface = $interface | .contract_sha256 = $contract |
        .descriptor.required_inputs |= (with_entries(select(.value)) | keys_unsorted | map(ascii_downcase)) |
        .descriptor.options_schema |= fromjson |
        .description = .descriptor.description |
        .engine = {name: .descriptor.name, version: .plugin_version}
    ' >"${output}/metadata.json"
jq '.descriptor.options_schema' "${output}/metadata.json" >"${output}/schema.json"
printf '%s\n' "${license}" >"${output}/LICENSE"
cp "${component}" "${output}/plugin.wasm"

# Custom sections can follow executable sections. The raw build has no discovery
# section; package a fresh build each time rather than appending to a distribution.
uleb32() {
	local value="$1" byte escaped
	while ((value >= 128)); do
		byte=$(((value & 127) | 128))
		printf -v escaped '\\%03o' "${byte}"
		printf '%b' "${escaped}"
		value=$((value >> 7))
	done
	printf -v escaped '\\%03o' "${value}"
	printf '%b' "${escaped}"
}
section='actionlint.plugin'
size="$(wc -c <"${output}/metadata.json")"
{
	printf '\0'
	uleb32 "$((1 + ${#section} + size))"
	uleb32 "${#section}"
	printf '%s' "${section}"
	cat "${output}/metadata.json"
} >>"${output}/plugin.wasm"
wasm-tools validate "${output}/plugin.wasm"
(
	cd "${output}"
	sha256sum plugin.wasm metadata.json schema.json LICENSE >SHA256SUMS
)

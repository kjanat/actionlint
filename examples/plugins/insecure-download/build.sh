#!/usr/bin/env bash
# Builds the insecure-download plugin component. Usage: build.sh [output.wasm]
# Requires tinygo, wasm-tools and go; composes TinyGo's WASI imports with the plugin world.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
output="${1:-insecure-download.wasm}"
contract="${ACTIONLINT_WIT:-../../../plugins/wit}"
tinygo_root="$(tinygo env TINYGOROOT)"

wasm-tools component wit "${contract}" --json | go tool wit-bindgen-go generate --world plugin --out gen

compose="$(mktemp -d)"
trap 'rm -rf "${compose}"' EXIT
mkdir -p "${compose}/deps/actionlint" "${compose}/deps/cli"
cp "${contract}"/*.wit "${compose}/deps/actionlint/"
cp "${tinygo_root}"/lib/wasi-cli/wit/*.wit "${compose}/deps/cli/"
cp -R "${tinygo_root}"/lib/wasi-cli/wit/deps/. "${compose}/deps/"
cat >"${compose}/world.wit" <<'WIT'
package example:insecure-download;

world insecure-download {
    include wasi:cli/imports@0.2.0;
    include kjanat:actionlint/plugin@0.1.0;
}
WIT

tinygo build -target=wasip2 -trimpath -buildvcs=false -wit-package "${compose}" -wit-world insecure-download -o "${output}" .
wasm-tools validate "${output}"
chmod 644 "${output}"

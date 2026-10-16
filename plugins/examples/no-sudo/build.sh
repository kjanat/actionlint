#!/usr/bin/env bash
# Builds the no-sudo plugin component. Usage: build.sh [output.wasm]
# Requires wit-bindgen, wasm-tools and wasi-sdk 25 or newer (WASI_SDK_PATH).
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
output="${1:-no-sudo.wasm}"
contract="${ACTIONLINT_WIT:-../../wit}"
sdk="${WASI_SDK_PATH:-/opt/wasi-sdk}"

wit-bindgen c --world plugin --out-dir gen "${contract}"
"${sdk}/bin/wasm32-wasip2-clang" -std=c11 -O2 -Wall -Wextra -Werror -mexec-model=reactor \
	-o "${output}" no_sudo.c gen/plugin.c gen/plugin_component_type.o
wasm-tools validate "${output}"

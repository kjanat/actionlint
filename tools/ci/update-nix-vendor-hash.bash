#!/usr/bin/env bash
set -euo pipefail

# Compute the Go vendor hash from scratch and write it into the package.
# A fake hash forces Nix to rebuild the module derivation: with the real
# hash, an old output in the store or a cache would satisfy the build and
# hide a stale value.
package=${1:-distribution/nix/package.nix}
fake=sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=

current=$(sed -nE 's/^  vendorHash = "(sha256-[A-Za-z0-9+/=]+)";$/\1/p' "${package}")
if [[ -z "${current}" ]]; then
	echo "${package} has no vendorHash line to update" >&2
	exit 1
fi

set_hash() {
	sed -i "s#^  vendorHash = \"$1\";\$#  vendorHash = \"$2\";#" "${package}"
}

set_hash "${current}" "${fake}"
trap 'set_hash "${fake}" "${current}"' EXIT

system=$(nix eval --impure --raw --expr builtins.currentSystem)
if log=$(nix build --no-link --no-update-lock-file ".#packages.${system}.default.goModules" 2>&1); then
	echo "goModules built with a fake vendorHash; cannot determine the real hash" >&2
	exit 1
fi

got=$(sed -nE 's/^[[:space:]]*got:[[:space:]]*(sha256-[A-Za-z0-9+/=]+)[[:space:]]*$/\1/p' <<<"${log}" | tail -n 1)
if [[ -z "${got}" ]]; then
	# The build failed for a reason other than the hash.
	printf '%s\n' "${log}" >&2
	exit 1
fi

trap - EXIT
set_hash "${fake}" "${got}"
if [[ "${got}" != "${current}" ]]; then
	echo "Updated vendorHash: ${current} -> ${got}"
fi

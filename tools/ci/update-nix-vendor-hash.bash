#!/usr/bin/env bash
set -euo pipefail

# Rebuild the Go module derivation and write the hash Nix reports back into
# the package. A matching hash builds and leaves the file unchanged.
package=${1:-distribution/nix/package.nix}

current=$(sed -nE 's/^  vendorHash = "(sha256-[A-Za-z0-9+/=]+)";$/\1/p' "${package}")
if [[ -z "${current}" ]]; then
	echo "${package} has no vendorHash line to update" >&2
	exit 1
fi

system=$(nix eval --impure --raw --expr builtins.currentSystem)
if log=$(nix build --no-link --no-update-lock-file ".#packages.${system}.default.goModules" 2>&1); then
	exit 0
fi

got=$(sed -nE 's/^[[:space:]]*got:[[:space:]]*(sha256-[A-Za-z0-9+/=]+)[[:space:]]*$/\1/p' <<<"${log}" | tail -n 1)
if [[ -z "${got}" ]]; then
	# The build failed for a reason other than the hash.
	printf '%s\n' "${log}" >&2
	exit 1
fi
sed -i "s#vendorHash = \"${current}\";#vendorHash = \"${got}\";#" "${package}"
echo "Updated vendorHash: ${current} -> ${got}"

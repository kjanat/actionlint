#!/usr/bin/env bash
set -euo pipefail

# Run from the repository root. The edited file must define the build target.
if (($# != 0)); then
	echo "Usage: bash tools/ci/update-nix-vendor-hash.bash" >&2
	exit 1
fi
package=distribution/nix/package.nix
fake=sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
hash_pattern='^sha256-[A-Za-z0-9+/]{43}=$'

read_hash() {
	sed -nE 's/^  vendorHash = "(sha256-[A-Za-z0-9+/=]+)";$/\1/p' "$1"
}

current=$(read_hash "${package}")
if [[ ! "${current}" =~ ${hash_pattern} ]]; then
	echo "${package} must contain exactly one SHA-256 vendorHash" >&2
	exit 1
fi

# Lock inputs whose flake.nix entry changed; other inputs stay pinned.
nix flake lock

scratch=$(mktemp -d "${package%/*}/.vendor-hash.XXXXXX")
restore=false
cleanup() {
	local status=$?
	if [[ "${restore}" == true ]] && ! mv -f -- "${scratch}/original" "${package}"; then
		echo "Cannot restore ${package}; the original is at ${scratch}/original" >&2
		exit 1
	fi
	rm -f -- "${scratch}/original" "${scratch}/candidate" "${scratch}/build.log"
	rmdir -- "${scratch}"
	return "${status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cp -p -- "${package}" "${scratch}/original"

write_hash() {
	cp -p -- "${scratch}/original" "${scratch}/candidate"
	sed "s#^  vendorHash = \"${current}\";\$#  vendorHash = \"$1\";#" \
		"${scratch}/original" >"${scratch}/candidate"
	if [[ "$(read_hash "${scratch}/candidate")" != "$1" ]]; then
		echo "Cannot replace vendorHash in ${package}" >&2
		return 1
	fi
	mv -f -- "${scratch}/candidate" "${package}"
}

system=$(nix eval --impure --raw --expr builtins.currentSystem)
target=".#packages.${system}.default.goModules"
restore=true
write_hash "${fake}"
drv=$(nix eval --raw --no-update-lock-file "${target}.drvPath")
if [[ -z "${drv}" ]]; then
	echo "Cannot determine the goModules derivation" >&2
	exit 1
fi

if nix build --no-link --no-update-lock-file --print-build-logs --log-format raw "${target}" 2>&1 | tee "${scratch}/build.log"; then
	echo "goModules built with a fake vendorHash; cannot determine the real hash" >&2
	exit 1
else
	statuses=("${PIPESTATUS[@]}")
fi
if ((statuses[0] == 0 || statuses[1] != 0)); then
	echo "Cannot capture the goModules build log" >&2
	exit 1
fi

got=$(awk -v drv="${drv}" -v fake="${fake}" '
	BEGIN { quote = sprintf("%c", 39) }
	{
		if (state == 2 && $1 == "got:" && NF == 2) {
			hash = $2
			matches++
		}
		if (state == 1 && $1 == "specified:" && $2 == fake && NF == 2)
			state = 2
		else
			state = index($0, "error: hash mismatch in fixed-output derivation " quote drv quote ":") == 1
	}
	END { if (matches == 1) print hash }
' "${scratch}/build.log")
if [[ ! "${got}" =~ ${hash_pattern} || "${got}" == "${fake}" ]]; then
	echo "No vendor hash mismatch for ${drv} with the expected fake hash" >&2
	exit 1
fi

write_hash "${got}"
nix build --no-link --no-update-lock-file "${target}"
restore=false
if [[ "${got}" != "${current}" ]]; then
	echo "Updated vendorHash: ${current} -> ${got}"
fi

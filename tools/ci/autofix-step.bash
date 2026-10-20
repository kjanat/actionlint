#!/usr/bin/env bash
set -euo pipefail

output_file=${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}

temporary=$(mktemp -d)
trap 'rm -f "${temporary}/index" "${temporary}/index.lock"; rmdir "${temporary}"' EXIT

# Compare each step's changes, including new files, without touching the real index.
snapshot() {
	GIT_INDEX_FILE="${temporary}/index" git read-tree HEAD || return
	GIT_INDEX_FILE="${temporary}/index" git -c core.fileMode=false add --all || return
	GIT_INDEX_FILE="${temporary}/index" git write-tree
}

before=$(snapshot)
"$@"
after=$(snapshot)

if [[ "${before}" != "${after}" ]]; then
	echo 'changed=true' >>"${output_file}"
else
	echo 'changed=false' >>"${output_file}"
fi

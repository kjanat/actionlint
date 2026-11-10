#!/usr/bin/env bash
set -euo pipefail

# Use NUL-delimited paths and disable rename detection so moves cover both names.
git diff --name-only --no-renames -z "$1" "$2" -- | {
	tables=false
	readme=false
	while IFS= read -r -d '' path; do
		if [[ "${path}" == internal/* || "${path}" == cmd/actionlint/* ]]; then
			readme=true
		fi
		case "${path}" in
			.github/workflows/autofix.yml | tools/ci/autofix-inputs.bash | go.mod | go.sum)
				tables=true
				readme=true
				;;
			internal/model/actions/* | internal/input/references/actions/* | internal/model/contexts/* | internal/development/generators/* | tools/generate/webhook-payloads/* | package.json | bun.lock | internal/checking/executor/expressions/check.go | internal/checking/rule_index.go | internal/checking/rule_registry.go | internal/checking/rules/* | docs/rules.md)
				tables=true
				;;
			.mise.toml | mise.lock | README.md | tools/docs/*)
				readme=true
				;;
			*) ;;
		esac
	done
	printf 'tables=%s\nreadme=%s\n' "${tables}" "${readme}"
} >>"${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}"

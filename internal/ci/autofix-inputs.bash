#!/usr/bin/env bash
set -euo pipefail

# Use NUL-delimited paths and disable rename detection so moves cover both names.
git diff --name-only --no-renames -z "$1" "$2" -- | {
	tables=false
	readme=false
	while IFS= read -r -d '' path; do
		case "${path}" in
			.github/workflows/autofix.yml | internal/ci/autofix-inputs.bash | go.mod | go.sum)
				tables=true
				readme=true
				;;
			internal/engine/action_metadata.go | internal/engine/action_metadata_availability.go | internal/engine/action_runtime.go | internal/engine/action_runtimes.go | internal/webhook/webhooks.go | internal/workflow/context/availability.go | internal/actioncatalog/* | internal/engine/rule_events.go | internal/engine/rule_expression.go | internal/actioncatalog/metadata/* | internal/actioncatalog/generate/* | internal/webhook/generate/* | internal/workflow/context/generate/* | internal/rulecatalog/* | docs/rules.md | generate.go)
				tables=true
				;;
			.mise.toml | README.md | docs/screenshots/* | internal/documentation/check-readme/*)
				readme=true
				;;
			*) ;;
		esac
	done
	printf 'tables=%s\nreadme=%s\n' "${tables}" "${readme}"
} >>"${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}"

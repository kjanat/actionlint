#!/usr/bin/env bash
set -euo pipefail

# Use NUL-delimited paths and disable rename detection so moves cover both names.
git diff --name-only --no-renames -z "$1" "$2" -- | {
	tables=false
	readme=false
	while IFS= read -r -d '' path; do
		case "${path}" in
			.github/workflows/autofix.yml | scripts/autofix-inputs.bash | go.mod | go.sum)
				tables=true
				readme=true
				;;
			internal/engine/action_metadata.go | internal/engine/action_metadata_availability.go | internal/engine/action_runtime.go | internal/engine/action_runtimes.go | internal/engine/all_webhooks.go | internal/workflowcontext/availability.go | internal/engine/popular_actions.go | internal/actioncatalog/* | internal/engine/rule_events.go | internal/engine/rule_expression.go | scripts/generate-action-metadata/* | scripts/generate-popular-actions/* | scripts/generate-webhook-events/* | scripts/generate-availability/*)
				tables=true
				;;
			.mise.toml | README.md | docs/screenshots/* | scripts/check-readme/*)
				readme=true
				;;
			*) ;;
		esac
	done
	printf 'tables=%s\nreadme=%s\n' "${tables}" "${readme}"
} >>"${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}"

#!/usr/bin/env bash

retry() {
	local attempt status delay=2
	for attempt in 1 2 3 4; do
		if "$@"; then
			return 0
		else
			status=$?
		fi
		if ((attempt == 4)); then
			return "${status}"
		fi
		printf 'GitHub release request failed (exit %s); retrying in %ss (attempt %s/4).\n' "${status}" "${delay}" "$((attempt + 1))" >&2
		sleep "${delay}"
		delay=$((delay * 2))
	done
}

# Dynamic versions need metadata. Honor GitHub's quota headers before retrying;
# fail when the requested wait exceeds the bounded retry window.
github_api() {
	local response status http retry_after reset remaining delay attempt
	for attempt in 1 2 3; do
		response="$(mktemp)"
		if gh api --include "$1" >"${response}"; then
			awk 'body {print} /^\r?$/ {body=1}' "${response}"
			rm -f "${response}"
			return 0
		else
			status=$?
		fi
		http="$(awk 'NR == 1 {print $2}' "${response}")"
		retry_after="$(awk 'tolower($1) == "retry-after:" {gsub("\r", "", $2); print $2}' "${response}")"
		reset="$(awk 'tolower($1) == "x-ratelimit-reset:" {gsub("\r", "", $2); print $2}' "${response}")"
		remaining="$(awk 'tolower($1) == "x-ratelimit-remaining:" {gsub("\r", "", $2); print $2}' "${response}")"
		rm -f "${response}"
		if [[ "${http}" != 403 && "${http}" != 429 ]] || ((attempt == 3)); then return "${status}"; fi
		delay=$((60 * attempt))
		if [[ "${retry_after}" =~ ^[0-9]+$ ]]; then delay=${retry_after}; fi
		if [[ "${remaining}" == 0 && "${reset}" =~ ^[0-9]+$ ]]; then
			reset=$((reset - $(date +%s) + 1))
			if ((reset > delay)); then delay=${reset}; fi
		fi
		if ((delay > 300)); then
			printf 'GitHub API quota requires waiting %ss; retry after the reset or use a reviewed pinned version.\n' "${delay}" >&2
			return "${status}"
		fi
		printf 'GitHub API rate limited; waiting %ss before retry.\n' "${delay}" >&2
		sleep "${delay}"
	done
}

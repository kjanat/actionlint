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

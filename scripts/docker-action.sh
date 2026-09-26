#!/bin/sh

# Keep the published Docker Action's positional protocol around the ordinary CLI.
if [ "$#" -ne 9 ]; then
	printf '%s\n' '::error title=Invalid action input::The action received an unexpected number of inputs'
	exit 2
fi

# Unlike absent JavaScript inputs, empty positional values are invalid here.
for input in "format=$2" "shellcheck=$5" "pyflakes=$6" "fail-on-error=$9"; do
	if [ -z "${input#*=}" ]; then
		printf "::error title=Invalid action input::Input '%s' must not be empty\n" "${input%%=*}"
		exit 2
	fi
done

exec env \
	"INPUT_FILES=$1" \
	"INPUT_FORMAT=$2" \
	"INPUT_IGNORE=$3" \
	"INPUT_CONFIG-FILE=$4" \
	"INPUT_SHELLCHECK=$5" \
	"INPUT_PYFLAKES=$6" \
	"INPUT_WORKING-DIRECTORY=$7" \
	"INPUT_OUTPUT-FILE=$8" \
	"INPUT_FAIL-ON-ERROR=$9" \
	actionlint -github-action

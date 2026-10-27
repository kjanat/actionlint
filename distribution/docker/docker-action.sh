#!/bin/sh

# Translate positional Action inputs to the ordinary CLI adapter.
if [ "$#" -ne 7 ]; then
	printf '%s\n' '::error title=Invalid action input::The action received an unexpected number of inputs'
	exit 2
fi

# Unlike absent JavaScript inputs, empty positional values are invalid here.
for input in "format=$2" "fail-on-error=$7"; do
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
	"INPUT_WORKING-DIRECTORY=$5" \
	"INPUT_OUTPUT-FILE=$6" \
	"INPUT_FAIL-ON-ERROR=$7" \
	actionlint -github-action

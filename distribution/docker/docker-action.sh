#!/bin/sh

# Positions 5 and 6 hold the shellcheck and pyflakes inputs. The script ignores them.
# The INPUT_DISABLE-TOOLS environment variable turns tools off.
if [ "$#" -ne 9 ]; then
	printf '%s\n' '::error title=Invalid action input::The action received an unexpected number of inputs'
	exit 2
fi

# Empty positional values are invalid here. Absent JavaScript inputs are valid.
for input in "format=$2" "fail-on-error=$9"; do
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
	"INPUT_WORKING-DIRECTORY=$7" \
	"INPUT_OUTPUT-FILE=$8" \
	"INPUT_FAIL-ON-ERROR=$9" \
	actionlint -github-action

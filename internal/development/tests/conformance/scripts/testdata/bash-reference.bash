#!/usr/bin/env bash
# Each case runs independently with the runner's explicit Bash arguments.
run_case() {
  label=$1
  script=$2
  printf '\nCASE %s\n' "$label"
  bash --noprofile --norc -e -o pipefail -c "$script"
  printf 'EXIT %s\n' "$?"
}
run_case assignment 'CHANGED="$(exit 7)"; EXIT_CODE=$?; echo CAPTURED:$EXIT_CODE'
run_case guarded 'CHANGED="$(exit 7)" || EXIT_CODE=$?; echo CAPTURED:$EXIT_CODE'
run_case disabled 'set +e; false; status=$?; echo CAPTURED:$status'
run_case and 'false && echo no; status=$?; echo CAPTURED:$status'
run_case compound 'if false; then :; fi; status=$?; echo CAPTURED:$status'
run_case trap 'trap '\''status=$?; echo TRAPPED:$status'\'' ERR; false'
run_case ignored_group '{ false; status=$?; echo CAPTURED:$status; } || :'
run_case subshell '(set +e; false; status=$?; echo INNER:$status); false; status=$?; echo OUTER:$status'
run_case substitution 'x=$(false; status=$?; echo INNER:$status); echo "$x"'

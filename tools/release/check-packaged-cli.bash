#!/usr/bin/env bash
# Exercise an installed binary or launcher: check-packaged-cli.bash COMMAND [ARG...]
# Requires bash, git and jq. Command paths must survive changing directories.
set -euo pipefail

smoke=$(mktemp -d)
trap 'rm -rf -- "${smoke}"' EXIT
cd "${smoke}"
git init --quiet
mkdir -p .github/workflows
cat >.github/workflows/clean.yml <<'YAML'
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
YAML

"$@" check .github/workflows/clean.yml --no-config --json >clean.json
jq -e '.schema_version == 1 and .diagnostics == []' clean.json

cat >broken.yml <<'YAML'
on: push
jobs: []
YAML
status=0
"$@" check broken.yml --no-config --json >broken.json || status=$?
test "${status}" -eq 1
jq -e '.schema_version == 1 and (.diagnostics | length > 0)' broken.json

cat >.github/actionlint.yaml <<'YAML'
plugins: []
config-variables: [PACKAGE_SMOKE]
YAML
"$@" config show --origin --json >config.json
jq -e '.config.lint.rules.correctness["expression-reference"].options.variables == ["PACKAGE_SMOKE"] and .origins["/lint/rules/correctness/expression-reference/options/variables"].source == "config"' config.json

"$@" rules --json >rules.json
jq -e 'type == "array" and any(.[]; .name == "syntax-check")' rules.json

for shell in bash zsh fish powershell; do
	"$@" completion "${shell}" >"${shell}.completion"
	test -s "${shell}.completion"
	grep -q '__complete' "${shell}.completion"
	# Package installers still use the legacy spelling; it must generate the same script.
	"$@" -completion "${shell}" >"${shell}.legacy"
	cmp "${shell}.completion" "${shell}.legacy"
done
echo 'Packaged CLI smoke checks passed.'

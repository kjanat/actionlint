{
  runCommand,
  git,
  actionlint,
}:

runCommand "actionlint-nix-integration" { nativeBuildInputs = [ git ]; } ''
  ${actionlint}/bin/actionlint -version | grep -Fx 'actionlint.kjanat.dev ${actionlint.version}'
  ${actionlint}/bin/actionlint -help > help.txt 2>&1
  grep -F 'Usage: actionlint' help.txt

  for file in \
    share/man/man1/actionlint.1.gz \
    share/bash-completion/completions/actionlint.bash \
    share/zsh/site-functions/_actionlint \
    share/fish/vendor_completions.d/actionlint.fish \
    share/actionlint/actionlint.schema.json; do
    test -s "${actionlint}/$file"
  done

  git init --quiet project
  mkdir -p project/.github/workflows
  cp ${./integration.yml} project/.github/workflows/check.yml
  cd project
  ${actionlint}/bin/actionlint -init-config
  grep -F 'yaml-language-server: $schema=' .github/actionlint.yaml

  set +e
  PATH= ${actionlint}/bin/actionlint -no-color -oneline > diagnostics.txt 2>&1
  status=$?
  set -e
  test "$status" -eq 1
  grep -F 'SC2086' diagnostics.txt
  grep -F "undefined name 'undefined_name'" diagnostics.txt

  touch "$out"
''

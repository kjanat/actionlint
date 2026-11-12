{
  runCommand,
  fetchurl,
  gitMinimal,
  jq,
  bash,
  actionlint,
}:

let
  shellcheck = fetchurl {
    url = "https://github.com/kjanat/actionlint-plugin-shellcheck/releases/download/v0.3.1/plugin.wasm";
    sha256 = "6c5ec0746a001b468283442ee7ddcacf5fa31bafc0ebe91ed2b4c9f05e71af31";
  };
  ruff = fetchurl {
    url = "https://github.com/kjanat/actionlint-plugin-ruff/releases/download/v0.3.0/plugin.component.wasm";
    sha256 = "b034362a59c097a4f85027941b4f902059668d1a087049d8704e365d265bc4e9";
  };
in
runCommand "actionlint-nix-integration"
  {
    nativeBuildInputs = [
      gitMinimal
      jq
      bash
    ];
  }
  ''
    bash ${../../tools/release/check-packaged-cli.bash} ${actionlint}/bin/actionlint
    ${actionlint}/bin/actionlint -version | grep -Fx 'actionlint.kjanat.dev/v2 ${actionlint.version}'
    expectedToolchain="$(sed -n 's/^toolchain //p' ${actionlint.src}/go.mod)"
    test -n "$expectedToolchain"
    ${actionlint}/bin/actionlint -version | grep -F "built with $expectedToolchain compiler for "
    ${actionlint}/bin/actionlint -help > help.txt 2>&1
    grep -Fx 'Usage:' help.txt
    grep -F 'actionlint [flags] [files...]' help.txt
    grep -Fx 'Docs: https://actionlint.kjanat.dev' help.txt
    ${actionlint}/bin/actionlint check --help > check-help.txt 2>&1
    grep -F 'actionlint check [flags] [--] [files... | -]' check-help.txt
    grep -F 'https://github.com/kjanat/actionlint/tree/v${actionlint.version}/docs/usage.md' check-help.txt

    for file in \
      share/man/man1/actionlint.1.gz \
      share/bash-completion/completions/actionlint.bash \
      share/zsh/site-functions/_actionlint \
      share/fish/vendor_completions.d/actionlint.fish \
      share/actionlint/actionlint.schema.json; do
      test -s "${actionlint}/$file"
    done

    while IFS= read -r -d "" schema; do
      jq -e . "$schema" > /dev/null
      while IFS= read -r reference; do
        case "$reference" in
          \#* | *:*) continue ;;
        esac
        target="$(dirname "$schema")/''${reference%%#*}"
        if ! test -s "$target"; then
          echo "Missing schema reference in $schema: $reference" >&2
          exit 1
        fi
      done < <(jq -r '.. | objects | ."$ref"? // empty' "$schema")
    done < <(find "${actionlint}/share/actionlint" -type f -name '*.json' -print0)

    git init --quiet project
    mkdir -p project/.github/workflows
    cp ${./integration.yml} project/.github/workflows/check.yml
    cd project
    set +e
    PATH= ${actionlint}/bin/actionlint check --no-config \
      --plugin ${shellcheck} --plugin ${ruff} \
      --json .github/workflows/check.yml > diagnostics.json 2> diagnostics.stderr
    status=$?
    set -e
    if ! test "$status" -eq 1 ||
      ! jq -e '.completed and .exit_code == 1
        and any(.diagnostics[]; .rule == "shellcheck" and .code == "SC2086")
        and any(.diagnostics[]; .rule == "ruff" and .code == "F821")' diagnostics.json; then
      cat diagnostics.json diagnostics.stderr >&2
      exit 1
    fi

    touch "$out"
  ''

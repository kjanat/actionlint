# C component example

This C plugin reports `sudo` at the start of a command in workflow scripts and composite action scripts. Quoted separators, comments, command substitutions and shell control structures can produce wrong or missed findings.

Build and package the component from the repository root. Packaging writes the release files to `plugins/examples/no-sudo/dist/`.

```sh
mise exec -- bash plugins/examples/no-sudo/build.sh
mise exec -- bash plugins/examples/no-sudo/package.sh
```

String values in [`descriptor.h`](descriptor.h) must be valid C and JSON string literals. Edit package identity and release information in [`metadata.json.in`](metadata.json.in). Set `ACTIONLINT_WIT` to an absolute path for both commands to build against another copy of the WIT contract.

Run the plugin with actionlint v2. The supplied workflow runs `sudo make install`. actionlint reports a warning and exits with status 0. Add `--strict` to make warnings fail the check with status 1.

```sh
actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/no-sudo/dist/plugin.wasm plugins/examples/no-sudo/example.yml
```

Build and run the tests in one command:

```sh
mise run test:example:no-sudo
```

A direct `go test` skips the execution tests unless `ACTIONLINT_TEST_BINARY` names the executable under test:

```sh
ACTIONLINT_TEST_BINARY="$PWD/actionlint" mise exec go -- go test ./plugins/examples/no-sudo/tests -count=1
```

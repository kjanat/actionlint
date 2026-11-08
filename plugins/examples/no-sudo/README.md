# C component example

This plugin demonstrates WIT bindings, script inputs and source-mapped findings
in C. It reports `sudo` at the start of a command, including commands preceded
by environment assignments. It checks workflow scripts and scripts supplied
from called composite actions. It does not read files, access the network or
offer fixes.

The scanner recognizes whitespace, newlines and the separators `;`, `|` and `&`.
It is not a Bash parser: quoted separators, comments, command substitutions and
shell control structures can produce incorrect or missed findings. It does not
determine whether a runner actually provides `sudo`. Use it to learn the plugin
interface, not to enforce a privilege policy.

From the repository root, build and package the component with the configured
WASI SDK, `wit-bindgen`, `wasm-tools` and `jq`:

```sh
mise exec -- bash plugins/examples/no-sudo/build.sh
mise exec -- bash plugins/examples/no-sudo/package.sh
```

The build generates C bindings from `plugins/protocol/wit`. Packaging writes
`plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS` under
`plugins/examples/no-sudo/dist/`.

The component and discovery metadata use the same declarations in
[`descriptor.h`](descriptor.h). String values in that header must be valid C
and JSON string literals. Edit package identity and release information in
[`metadata.json.in`](metadata.json.in).

The packager derives the interface version and contract digest from the WIT
used to build the component. Set `ACTIONLINT_WIT` for both commands when using
another copy of that contract.

Run it using actionlint v2:

```sh
actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/no-sudo/dist/plugin.wasm plugins/examples/no-sudo/example.yml
```

The supplied workflow runs `sudo make install`. The plugin reports that command
because self-hosted runners may not provide `sudo`. The finding makes the CLI
exit with status 1.

Build and run the example's tests in one command:

```sh
mise run test:example:no-sudo
```

This builds a native CLI and packages the component before running the tests. The tests
invoke the CLI and inspect its JSON report. They check clean
commands, command-name boundaries, assignments, Unicode columns, multiline
scripts, captured revisions and exit status. Set `ACTIONLINT_TEST_BINARY` to the
executable you want to test:

```sh
ACTIONLINT_TEST_BINARY="$PWD/actionlint" mise exec go -- go test ./plugins/examples/no-sudo/tests -count=1
```

In PowerShell:

```powershell
$env:ACTIONLINT_TEST_BINARY = "$PWD/actionlint.exe"
mise exec go -- go test ./plugins/examples/no-sudo/tests -count=1
```

Without that variable, a direct `go test` skips these execution tests. The host's component
suite separately checks called composite actions against its owned fixture.

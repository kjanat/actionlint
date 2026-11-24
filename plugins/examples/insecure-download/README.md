# Insecure-download plugin

This Go component reports simple `curl --insecure`, `curl -k` and `wget --no-check-certificate` commands in Bash and POSIX shell scripts. It parses only part of shell syntax.

Build the component from the repository root. The build needs Go, TinyGo and wasm-tools, and writes `insecure-download.wasm` beside this README.

```sh
mise exec -- bash plugins/examples/insecure-download/build.sh
```

Run the native tests:

```sh
mise exec go -- go -C plugins/examples/insecure-download test ./...
```

Create a release directory:

```sh
mise exec go -- go -C plugins/examples/insecure-download run . insecure-download.wasm dist
```

Try the component:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/insecure-download/insecure-download.wasm workflow.yml
```

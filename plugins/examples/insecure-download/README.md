# Insecure-download plugin

This Go component reports simple `curl --insecure`, `curl -k` and
`wget --no-check-certificate` commands in Bash and POSIX shell scripts. It is an
authoring example, not a complete shell parser.

From the actionlint repository root, build the registry-ready component:

```sh
mise exec -- bash plugins/examples/insecure-download/build.sh
```

The build needs Go, TinyGo and wasm-tools. It writes `insecure-download.wasm`
beside this README. The component includes its `actionlint.plugin` discovery
metadata. The native packaging entry point calls the same `describe()` function
as the guest, so its rule declaration is not copied into a separate manifest.

Run its native tests:

```sh
mise exec go -- go -C plugins/examples/insecure-download test ./...
```

Create a release directory with the component, metadata, option schema, license
and checksums:

```sh
mise exec go -- go -C plugins/examples/insecure-download run . insecure-download.wasm dist
```

The packaging entry point is in [main_native.go](main_native.go), excluded from
the Wasm build. [main_guest.go](main_guest.go) supplies the guest's empty main
function. The checking implementation and descriptor are in [main.go](main.go).

To try the component from this repository:

```sh
mise exec go -- go run ./cmd/actionlint check --no-config --no-tool shellcheck --no-tool ruff \
  --plugin plugins/examples/insecure-download/insecure-download.wasm workflow.yml
```

Replace `workflow.yml` with a workflow you want to check. A download command
that disables TLS verification produces an `insecure-download` warning.

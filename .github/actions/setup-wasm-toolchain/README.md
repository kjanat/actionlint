# Pinned WebAssembly fixture toolchain

This action installs the tools pinned in [`releases.json`](releases.json) for the
Linux x64 plugin fixture job. These CI pins differ from the local development
versions in [`.mise.toml`](../../../.mise.toml). The action reports an error on
unsupported runner platforms.

Both archives were downloaded and checked against their GitHub release asset
digests and `gh release verify-asset` on 2026-10-21. The authenticated tags resolve
to `5c6d31c78f8bd503f558441ef9c732950a141d1a` (wasm-tools) and
`c604ee01c03f50d7301984a3c920a650752cd136` (wit-bindgen).

Cold installs download public HTTPS assets directly, then verify the reviewed
SHA-256 pins before extraction or execution. Restored archives undergo the same
verification. No GitHub token or API request is needed. Updating `releases.json`
selects a fresh cache; verify new archives and their attestations before updating.

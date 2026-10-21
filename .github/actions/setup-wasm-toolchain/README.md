# Pinned WebAssembly fixture toolchain

This private action installs the two tools used by the Linux x64 plugin fixture
job: wasm-tools 1.258.0 and wit-bindgen 0.62.0. It preserves their versions and the
existing fixture builds and tests. Unsupported runner platforms fail explicitly.

Both archives were downloaded and checked against their GitHub release asset
digests and `gh release verify-asset` on 2026-10-21. The authenticated tags resolve
to `5c6d31c78f8bd503f558441ef9c732950a141d1a` (wasm-tools) and
`c604ee01c03f50d7301984a3c920a650752cd136` (wit-bindgen).

Cold installs download public HTTPS assets directly, then verify the reviewed
SHA-256 pins before extraction or execution. Restored archives undergo the same
verification. No GitHub token or API request is needed. Updating `releases.json`
selects a fresh cache; verify new archives and their attestations before updating.

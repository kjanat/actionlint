# Verified ShellCheck installation

The default 0.11.0 release uses `pinned-release.json`; cold and warm installs need
no GitHub API requests. All five archives were downloaded over HTTPS and matched
the upstream release asset SHA-256 metadata on 2026-10-21. The pins cover Linux and
macOS x64/arm64 and the Windows x64 binary, also used through ARM64 emulation.

The installer checks both the cached archive and its receipt against the committed
checksums. Changing the receipt cannot make an archive with a different checksum
pass verification. Changing the pins selects a new cache key.

For updates, verify the downloaded archives against upstream release metadata,
then review the version and checksums together. Other version inputs and `latest`
still resolve dynamically. That path honors GitHub's Retry-After and primary quota
reset headers. Waits exceeding five minutes fail with reset guidance.

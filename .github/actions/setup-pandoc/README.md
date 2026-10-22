# Pinned Pandoc installer

This private action downloads public release assets directly and checks their SHA-256
before installation, including after cache restoration. It needs no GitHub token or
release metadata requests.

Both Pandoc 3.11 Debian assets were verified with `gh release verify-asset 3.11`
against `jgm/pandoc` on 2026-10-21. GitHub's release attestation resolved that tag to
`b913622e1ff87c69ab8b1a606577122e220925cd` and authenticated the hashes in `SHA256SUMS`.

For updates, download both architectures, verify each asset's release attestation,
then update `version` in `setup.bash` and the reviewed `SHA256SUMS` together. Changing
an asset hash automatically selects a fresh cache entry. Never copy an unverified
download's hash into the pins to make a failed installation pass.

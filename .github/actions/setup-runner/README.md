# Private Runner installer

This repository pins Runner 0.28.0. Linux GNU, macOS and Windows x64/arm64
packages come from their exact npm tarball URLs. No GitHub API request or token
is needed to install them.

A cold install checks the committed archive size, SHA-256 and npm SHA-512
integrity. Before extraction, `gh attestation verify` verifies the bundled
Sigstore signature, transparency evidence and trusted roots. Policy requires
SLSA v1 provenance from `kjanat/runner`, tag `refs/tags/v0.28.0`, source and signer
commit `b32ea932a1813ae0be44e755c16df9f9f5ca86a4`, and the GitHub-hosted
`.github/workflows/npm-release.yml` signer. Verification runs with empty GitHub
credentials/configuration and unreachable network proxies.

Both `auto` and `require` verify these reviewed packages. Unlike the upstream
installer's `auto` fallback for absent provenance, this private installer fails
closed for versions/platforms without committed proofs. It never supports
`off`.

After verification, only `runner` and `run` are extracted. Their hashes must
match the committed manifest. Every cache reuse compares those same trusted
hashes before execution; editing a cache receipt cannot authorize different
binary bytes. Warm installation performs no network requests or provenance
subprocesses. Cache identity includes platform, policy, installer source,
manifest, proofs and trusted-root snapshot.

## Updating the pin

1. Confirm the immutable release tag and source commit. Fetch exact npm package
   metadata for all six platforms; record URL, size, SHA-512 integrity and
   independently computed SHA-256 in `proofs.mjs`.
2. Retrieve each package digest's signed GitHub SLSA v1 attestation from
   `repos/kjanat/runner/attestations/sha256:DIGEST`. Preserve the signed bundle
   under `proofs/VERSION/`; never modify its signed payload.
3. Obtain the public trust snapshot with `gh attestation trusted-root`. Review
   the provenance's repository, source tag/commit and signer workflow/commit.
   Update the explicit policy only when the release actually changes it.
4. Verify every tarball with `gh attestation verify --bundle BUNDLE
   --custom-trusted-root ROOT --repo kjanat/runner --source-ref TAG
   --source-digest COMMIT --signer-workflow WORKFLOW --signer-digest COMMIT
   --predicate-type https://slsa.dev/provenance/v1 --deny-self-hosted-runners`.
   Repeat with empty GitHub configuration/credentials and networking blocked.
5. Extract only after successful verification. Record SHA-256 for both binaries
   in the manifest. Run installer tests, including real cold/offline warm
   installation and rejection of corrupt archives, signatures and roots.
6. Review and commit the manifest, proofs and version pin together. Unsupported
   pins intentionally fail with instructions to update these proofs.

The current proofs identify build
[38110976942](https://github.com/kjanat/runner/actions/runs/38110976942/attempts/1).
Verification uses the official
[offline bundle flags](https://cli.github.com/manual/gh_attestation_verify) and
[trusted-root snapshot](https://cli.github.com/manual/gh_attestation_trusted-root)
commands.

# Playground for actionlint

Vite bundles the playground. The editor talks to the Go language server over LSP in a browser worker. [`libraries/browser-lsp`](../../libraries/browser-lsp/) builds that worker from `cmd/actionlint-lsp-wasm`.

## Tasks

Run these from `applications/playground`:

```sh
# Build the wasm binary and the bundle
bun run build

# Serve the built bundle at localhost:1234
bun run preview

# Run editor and source-sharing unit tests
bun run test

# Install Chromium once, then test the built site
bunx playwright install chromium
bun run test:browser

bun run lint
```

`bun run dev` builds the server package and starts Vite with hot reloading.

The playground checks one YAML buffer. It loads no repository configuration and no remote action metadata.

`@kjanat/rshellcheck-wasm` checks shell scripts in the browser. The browser loads it from version-pinned jsDelivr URLs, and a CDN failure stops startup. The npm development dependency supplies the same version for offline tests. ShellCheck runs in a worker with a ten-second deadline. The playground does not check Python or PowerShell scripts.

The ShellCheck module is GPL-3.0-or-later. The footer links its source at the npm provenance commit.

Vite derives the version badge and links from Git at startup. A clean release tag links to its release. Other builds show `git describe` and link to their commit. Set `ACTIONLINT_LATEST_RELEASE` to show a latest-release link in a local build.

## Deployment

The [Pages workflow](../../.github/workflows/pages.yml) deploys `dist/` after successful Release runs and on path-filtered `master` pushes.

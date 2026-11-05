# Playground for actionlint

This is a development directory for [actionlint playground](https://kjanat.github.io/actionlint/).

The playground is built with HTML/CSS/TypeScript/Wasm and bundled by [Vite](https://vite.dev/). All dependencies are
defined in the repository's Bun workspace. The editor connects to the actual Go language
server through standard LSP messages in a browser worker.

## Layout

| Path             | Contents                                            |
| ---------------- | --------------------------------------------------- |
| `index.html`     | Vite entry point                                    |
| `src/`           | TypeScript sources and the stylesheet               |
| `public/`        | Static site assets                                  |
| `dist/`          | Bundle produced by `vite build`                     |
| `vite.config.ts` | Build and test configuration                        |
| `tests/`         | Chromium tests of the built editor and real workers |

The server package lives in [`libraries/browser-lsp`](../../libraries/browser-lsp/).
Its build compiles `cmd/actionlint-lsp-wasm` and copies the matching Go runtime.
Vite bundles that worker and its Wasm into the site. The package also exposes the
worker for a future VS Code web extension.

## Tasks

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
```

`bun run dev` builds the server package and starts Vite with hot reloading.

The playground supplies one YAML buffer to the server. It does not load repository
configuration or fetch remote action metadata. Remote references receive syntax checks;
input and output validation needs metadata that this single-buffer host cannot supply.
Other browser hosts can provide closed local files through the package's file-read request.

The server publishes versioned diagnostics with UTF-16 ranges, severity, diagnostic
codes and related locations. CodeMirror displays them and requests completions and
quick fixes through LSP. The editor applies a fix only when it changes the current
buffer at its current version. Multi-file and stale fixes are rejected.

Shell scripts are checked locally with `@kjanat/rshellcheck-wasm` 1.0.0. The browser loads its
worker, JavaScript wrapper and WASM from version-pinned jsDelivr URLs. A same-origin module-worker
bootstrap imports the CDN worker. Scripts stay in the browser; they are not sent to an analysis server.
CDN download or initialization failures stop startup with an error. The npm development dependency
supplies the same version for offline tests, and its runtime assets are not bundled with the site.
The ordinary Go ShellCheck rule handles shell selection, expression placeholders, exclusions,
inline suppressions and YAML source ranges. A small adapter maps its CLI arguments to the WASM
request format and rejects unsupported flags. Repository source files and rc files are unavailable.
Python and PowerShell scripts are not sent to ShellCheck; Ruff is not included in this playground.

ShellCheck runs in a dedicated worker with a ten-second deadline. Worker failures and incomplete
analysis produce error messages. Every edit immediately invalidates the previous analysis, clears
its results and notifies the language server, which cancels the previous analysis. The next analysis
can restart the worker with a fresh deadline. The separate ShellCheck
module is GPL-3.0-or-later; the footer links its corresponding source at the npm provenance commit.

Vite derives the version badge and links from Git when it starts. A clean release tag links to its release; development
builds show `git describe` and link to their commit. Documentation links use that same commit. Without tags, the badge
shows the commit; without Git metadata, it shows `development`. Pages also reads the latest published release and displays
a separate release link when the release API is available. Local builds can supply that tag through `ACTIONLINT_LATEST_RELEASE`; without it, only the source
badge appears. The release label does not identify the built WASM. No release version is maintained in `index.html`.

## Lint

Sources are linted with [eslint](https://eslint.org/) with [typescript-eslint](https://github.com/typescript-eslint/typescript-eslint),
and type-checked separately:

```sh
bun run lint
```

Formatting is handled from the repository root by kprint. See [`dprint.toml`](../../dprint.toml).

## Deployment

The [Pages workflow](../../.github/workflows/pages.yml) builds the WASM and site from the same checkout and deploys `dist/`
on pushes to `master` and after successful Release runs. Release-triggered builds use the current default branch with
its tags. The UI labels the source revision and latest published release separately. See
[CONTRIBUTING.md](../../CONTRIBUTING.md) for more details.

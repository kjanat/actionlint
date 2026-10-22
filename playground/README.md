# Playground for actionlint

This is a development directory for [actionlint playground](https://kjanat.github.io/actionlint/).

The playground is built with HTML/CSS/TypeScript/Wasm and bundled by [Vite](https://vite.dev/). All dependencies are
defined in `package.json` and managed by `npm`, which also defines the development tasks.

## Layout

| Path             | Contents                                             |
| ---------------- | ---------------------------------------------------- |
| `index.html`     | Vite entry point                                     |
| `src/`           | TypeScript sources, ambient types and the stylesheet |
| `public/`        | `main.wasm`, built by Vite from `main.go`            |
| `dist/`          | Bundle produced by `vite build`                      |
| `vite.config.ts` | Build and test configuration                         |

## Tasks

```sh
# Build the wasm binary and the bundle
npm run build

# Serve the built bundle at localhost:1234
npm run preview

# Build the wasm binary, then run tests
npm test
```

`npm run dev` builds `public/main.wasm` and starts the Vite dev server with hot reloading.

The playground fetches `internal/actioncatalog/popular_actions.json` separately through Vite's asset pipeline.
Production builds give the JSON a content-hashed URL. WASM excludes the embedded catalogue and checks the fetched
bytes against its build-time SHA-256 before exposing the lint function. A failed fetch or mismatched deployment
stops startup and reports an error. Ordinary CLI and library builds still embed the catalogue.

Shell scripts are checked locally with `@kjanat/rshellcheck-wasm` 0.11.0. The browser loads its
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
its results and cancels busy worker requests before the debounce timer starts. The next analysis
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
npm run lint
```

Formatting is handled from the repository root by [dprint](https://dprint.dev/). See [`dprint.toml`](../dprint.toml).

## Deployment

The [Pages workflow](../.github/workflows/pages.yml) builds the WASM and site from the same checkout and deploys `dist/`
on pushes to `master` and after successful Release runs. Release-triggered builds use the current default branch with
its tags. The source revision and latest published release remain separately labeled. See
[CONTRIBUTING.md](../CONTRIBUTING.md) for more details.

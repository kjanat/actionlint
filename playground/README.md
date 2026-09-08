# Playground for actionlint

This is a development directory for [actionlint playground](https://kjanat.github.io/actionlint/).

The playground is built with HTML/CSS/TypeScript/Wasm and bundled by [Vite](https://vite.dev/). All dependencies are
defined in `package.json` and managed by `npm`. Tasks for development are defined in [`Makefile`](./Makefile).

## Layout

| Path             | Contents                                                          |
| ---------------- | ----------------------------------------------------------------- |
| `index.html`     | Vite entry point                                                  |
| `src/`           | TypeScript sources, ambient types and the stylesheet              |
| `public/`        | `main.wasm` and the Go toolchain's `wasm_exec.js`, both generated |
| `dist/`          | Bundle produced by `vite build`                                   |
| `vite.config.ts` | Build and test configuration                                      |

## Tasks

```sh
# Install dependencies, build the wasm binary and the bundle, then serve it at localhost:1234
make serve

# Install dependencies, build the wasm binary and the bundle
make build

# Install dependencies
make dep

# Run tests
make test

# Clean all built files
make clean
```

`npm run dev` starts the Vite dev server with hot reloading against an already built `public/main.wasm`.

Vite derives the version badge and links from Git when it starts. A clean release tag links to its release; development
builds show `git describe` and link to their commit. Documentation links use that same commit. Without tags, the badge
shows the commit; without Git metadata, it shows `development`. No release version is maintained in `index.html`.

## Lint

Sources are linted with [eslint](https://eslint.org/) with [typescript-eslint](https://github.com/typescript-eslint/typescript-eslint),
and type-checked separately:

```sh
npm run lint
```

Formatting is handled from the repository root by [dprint](https://dprint.dev/). See [`.dprint.jsonc`](../.dprint.jsonc).

## Deployment

The [Pages workflow](../.github/workflows/pages.yml) builds the WASM and site from the same checkout and deploys `dist/`
on pushes to `master` and after successful Release runs. The release-triggered build checks out the release commit with
its tags, so the badge updates once the release exists. See
[CONTRIBUTING.md](../CONTRIBUTING.md) for more details.

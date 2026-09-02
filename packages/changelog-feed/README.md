# `@kjlint/changelog-rss`

RSS 2.0 parser, browser reader, and issue monitor for the [GitHub Actions changelog feed].

## Feed parser

Import the side-effect-free parser from the `feed` subpath:

```js
import { FEED_URL, parseFeed } from '@kjlint/changelog-rss/feed';

const response = await fetch(FEED_URL);
const feed = parseFeed(await response.text());
```

`parseFeed()` returns the channel metadata and normalized entries, including
GitHub's changelog type and label categories.

## Browser reader

Importing the package root starts the browser reader:

```js
import '@kjlint/changelog-rss';
```

The reader expects the DOM structure from
[`playground/github-changelog/index.html`](../../playground/github-changelog/index.html).
It loads the current feed, caches entries in IndexedDB, and wires up filtering,
pagination, refresh, and article rendering.

The same entrypoint can be loaded from an ESM CDN:

```html
<script type="module" src="https://esm.sh/@kjlint/changelog-rss"></script>
<script type="module" src="https://esm.run/@kjlint/changelog-rss"></script>
```

Pin a published version in production deployments.

## GitHub Actions monitor

The `action` subpath exports the function used by the repository's composite action.
It is designed to run inside `actions/github-script`:

```js
const { default: run } = require('@kjlint/changelog-rss/action');
await run({ github, context, core });
```

## Exports

| Import                               | Purpose                           |
| ------------------------------------ | --------------------------------- |
| `@kjlint/changelog-rss`              | Start the browser reader          |
| `@kjlint/changelog-rss/action`       | Run the changelog issue monitor   |
| `@kjlint/changelog-rss/feed`         | Parse the GitHub Actions RSS feed |
| `@kjlint/changelog-rss/reader`       | Start the browser reader directly |
| `@kjlint/changelog-rss/package.json` | Read package metadata             |

## Development

The source is TypeScript under `src/`, compiled to `dist/` by `tsc`. The tests run
against `src/` directly on Node's type stripping.

```sh
run test:feed
run --dir packages/changelog-feed build
```

`npm pack` and `npm publish` build first through `prepack`.

[GitHub Actions changelog feed]: https://github.blog/changelog/label/actions/feed/

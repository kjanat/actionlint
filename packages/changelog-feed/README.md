# `@kjlint/changelog-rss`

RSS 2.0 parser and browser reader for the [GitHub Actions changelog feed].

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

After the package is publicly published, the same entrypoint can be loaded from
an ESM CDN:

```html
<script type="module" src="https://esm.run/@kjlint/changelog-rss"></script>
```

Pin a published version in production deployments.

## Exports

| Import                               | Purpose                           |
| ------------------------------------ | --------------------------------- |
| `@kjlint/changelog-rss`              | Start the browser reader          |
| `@kjlint/changelog-rss/feed`         | Parse the GitHub Actions RSS feed |
| `@kjlint/changelog-rss/reader`       | Start the browser reader directly |
| `@kjlint/changelog-rss/package.json` | Read package metadata             |

## Development

Run the package tests from the repository root:

```sh
npm run test:feed
```

[GitHub Actions changelog feed]: https://github.blog/changelog/label/actions/feed/

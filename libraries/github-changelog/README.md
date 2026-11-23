# `@kjlint/changelog-rss`

| Import                         | Purpose                           |
| ------------------------------ | --------------------------------- |
| `@kjlint/changelog-rss`        | Start the browser reader          |
| `@kjlint/changelog-rss/action` | Run the changelog issue monitor   |
| `@kjlint/changelog-rss/feed`   | Parse the GitHub Actions RSS feed |

## Feed parser

The `feed` subpath has no side effects:

```js
import { FEED_URL, parseFeed } from '@kjlint/changelog-rss/feed';

const response = await fetch(FEED_URL);
const feed = parseFeed(await response.text());
```

## Browser reader

```js
import '@kjlint/changelog-rss';
```

The reader expects the DOM of [`applications/playground/github-changelog/index.html`](../../applications/playground/github-changelog/index.html) and caches entries in IndexedDB.

## GitHub Actions monitor

Run the monitor inside `actions/github-script`:

```js
const { default: run } = require('@kjlint/changelog-rss/action');
await run({ github, context, core });
```

## Development

`tsc` compiles `src/` to `dist/`. The tests run against `src/` with Node's TypeScript type stripping.

```sh
run -s @kjlint/changelog-rss:typecheck @kjlint/changelog-rss:test
run --dir libraries/github-changelog build
```

## Publishing

`changelog-release.yml` publishes the package through the `npm` environment. A manual dispatch defaults to a dry run. Bump the workspace version before you publish.

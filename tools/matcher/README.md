# generate-actionlint-matcher

This script generates [`actionlint-matcher.json`](../../.github/actionlint-matcher.json).

## Usage

```sh
mise exec -- run matcher
```

To run the script directly:

```sh
node ./tools/matcher/main.mjs .github/actionlint-matcher.json
```

## Test

```sh
run //tools/matcher:test
```

This task runs `node --test`. The same tests also run as part of `run test:js`.

The test uses text-format fixtures at `./tools/matcher/testdata/*.txt`. The matcher reads `--output-format text`,
not the default pretty output. Regenerate the text and JSON expectations together:

```sh
run matcher:fixtures
```

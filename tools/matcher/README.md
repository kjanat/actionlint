# generate-actionlint-matcher

This script generates [`actionlint-matcher.json`](../../../.github/actionlint-matcher.json).

## Usage

```sh
make .github/actionlint-matcher.json
```

or directly run the script

```sh
node ./tools/matcher/main.mjs .github/actionlint-matcher.json
```

## Test

```sh
run //tools/matcher:test
```

It runs on `node --test` and is also covered by `run test:js`.

The test uses text-format fixtures at `./tools/matcher/testdata/*.txt`. The matcher reads `--output-format text`,
not the default pretty output. Regenerate the text and JSON expectations together:

```sh
run matcher:fixtures
```

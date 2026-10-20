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

The test uses test data at `./tools/matcher/testdata/*.txt`. They should be updated when actionlint changes
the default error message format. To update them:

```sh
make ./tools/matcher/testdata/escape.txt
make ./tools/matcher/testdata/no_escape.txt
make ./tools/matcher/testdata/want.json
```

or expand glob by your shell:

```sh
make ./tools/matcher/testdata/*
```

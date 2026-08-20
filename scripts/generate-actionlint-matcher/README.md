# generate-actionlint-matcher

This script generates [`actionlint-matcher.json`](../../.github/actionlint-matcher.json).

## Usage

```sh
make .github/actionlint-matcher.json
```

or directly run the script

```sh
node ./scripts/generate-actionlint-matcher/main.mjs .github/actionlint-matcher.json
```

## Test

```sh
node ./scripts/generate-actionlint-matcher/test.mjs
```

The test uses test data at `./scripts/generate-actionlint-matcher/testdata/*.txt`. They should be updated when actionlint changes
the default error message format. To update them:

```sh
make ./scripts/generate-actionlint-matcher/testdata/escape.txt
make ./scripts/generate-actionlint-matcher/testdata/no_escape.txt
make ./scripts/generate-actionlint-matcher/testdata/want.json
```

or expand glob by your shell:

```sh
make ./scripts/generate-actionlint-matcher/testdata/*
```

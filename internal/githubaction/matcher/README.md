# generate-actionlint-matcher

This script generates [`actionlint-matcher.json`](../../../.github/actionlint-matcher.json).

## Usage

```sh
make .github/actionlint-matcher.json
```

or directly run the script

```sh
node ./internal/githubaction/matcher/main.mjs .github/actionlint-matcher.json
```

## Test

```sh
run //internal/githubaction/matcher:test
```

It runs on `node --test` and is also covered by `run test:js`.

The test uses test data at `./internal/githubaction/matcher/testdata/*.txt`. They should be updated when actionlint changes
the default error message format. To update them:

```sh
make ./internal/githubaction/matcher/testdata/escape.txt
make ./internal/githubaction/matcher/testdata/no_escape.txt
make ./internal/githubaction/matcher/testdata/want.json
```

or expand glob by your shell:

```sh
make ./internal/githubaction/matcher/testdata/*
```

# actionlint

[actionlint](https://github.com/kjanat/actionlint) checks GitHub Actions workflows.

## Install

```sh
python -m pip install actionlint-kjanat
```

With uv:

```sh
uv add --dev actionlint-kjanat
uv run actionlint
```

Without adding a dependency:

```sh
uvx actionlint-kjanat
```

The package installs `actionlint` and `actionlint-kjanat` as two names for the same executable.

## Check your workflows

Run actionlint in your Git repository:

```sh
actionlint
```

```sh
actionlint .github/workflows/ci.yml
```

See the [usage guide](https://github.com/kjanat/actionlint/blob/v{{version}}/docs/usage.md) and the [checks list](https://github.com/kjanat/actionlint/blob/v{{version}}/docs/checks.md).

## Supported platforms

Wheels exist for Linux with glibc or musl, macOS and Windows. On other platforms, use an archive from the [v{{version}} release](https://github.com/kjanat/actionlint/releases/tag/v{{version}}).

## Support development

[Sponsor Kaj Kowalski](https://github.com/sponsors/kjanat). Report problems in the [issue tracker](https://github.com/kjanat/actionlint/issues).

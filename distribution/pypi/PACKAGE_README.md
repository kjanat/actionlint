# actionlint

[actionlint](https://github.com/kjanat/actionlint) checks GitHub Actions workflows for YAML errors,
invalid expressions, incorrect action inputs and unsafe script interpolation.

## Install

```sh
python -m pip install actionlint-kjanat
```

The package installs the `actionlint` command. pip selects the executable for your operating system and CPU.
You do not need Go or a compiler.

With uv, add actionlint as a development dependency and run it in the project:

```sh
uv add --dev actionlint-kjanat
uv run actionlint
```

To run it without adding a dependency:

```sh
uvx actionlint-kjanat
```

The package installs two commands, `actionlint` and `actionlint-kjanat`, that run the same actionlint executable.

## Check your workflows

Run actionlint from your Git repository:

```sh
actionlint
```

To check a specific file:

```sh
actionlint .github/workflows/ci.yml
```

Use `actionlint -h` for command options. See the [usage guide](https://github.com/kjanat/actionlint/blob/v{{version}}/docs/usage.md) for configuration and the [checks list](https://github.com/kjanat/actionlint/blob/v{{version}}/docs/checks.md) for examples.
To check embedded shell and Python scripts, follow the script-linter setup instructions in the usage guide.

## Supported platforms

Wheels are available for Linux with glibc or musl, macOS and Windows.
Each wheel includes the executable from the [v{{version}} release](https://github.com/kjanat/actionlint/releases/tag/v{{version}}).
If pip cannot find a wheel for your platform, use an archive from that release.

## Support development

[Sponsor Kaj Kowalski](https://github.com/sponsors/kjanat) to support actionlint development.
Report problems in the [issue tracker](https://github.com/kjanat/actionlint/issues).

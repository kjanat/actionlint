# Documentation checker

This tool maintains [the Checks document](../../../../../docs/checks.md).

The checker can:

- Update the example output in the code blocks after `Output:`.
- Update each example's [playground](https://kjanat.github.io/actionlint/) link.
- Check whether the recorded output and links match the examples.

Generated examples use a fixed repository path and consistent missing-file error
messages on Windows, Linux, and macOS.

## Prerequisites

- Go
- `shellcheck` command

## Usage

```sh
go run ./internal/development/tests/integration/documentation [FLAGS] FILE
```

Check the recorded output and playground links without modifying the document.

```sh
go run ./internal/development/tests/integration/documentation ./docs/checks.md
```

Update the document. This command directly modifies the file.

```sh
go run ./internal/development/tests/integration/documentation -fix ./docs/checks.md
```

The check is run on the [CI workflow](../../../../../.github/workflows/ci.yml).

## Format

Each section has this format:

````markdown
<a id="some-id"></a>

## This is title of the check

Example input:

```yaml
# This section is referred to generate the output and the playground link
on: push
jobs:
  test:
    runs-on: linux-latest # oops
    steps:
      - run: echo hello
```

Output:

```
THIS CODE BLOCK WILL BE AUTO-GENERATED
```

[Playground](URL_WILL_BE_AUTO_GENERATED)

Explanation for the check...
````

One section can contain several examples of the same rule.

The example input code block must not be empty and must contain at least one error.

To write an example's output manually, add the following comment before its output
code block. The checker will not update that block.

```yaml
Output:
<!-- Skip update output -->
```

To omit an example's playground link, replace it with the following comment.
The checker will not generate a link for that example.

```yaml
<!-- Skip playground link -->
```

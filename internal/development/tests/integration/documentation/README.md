# Documentation checker

## Usage

Check the document:

```sh
go run ./internal/development/tests/integration/documentation ./docs/checks.md
```

Update the document:

```sh
go run ./internal/development/tests/integration/documentation -fix ./docs/checks.md
```

## Format

````markdown
<a id="some-id"></a>

## This is title of the check

Example input:

```yaml
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

Each example input must report at least one error.

Keep a hand-written output with this comment:

```markdown
Output:

<!-- Skip update output -->
```

Omit a playground link with this comment:

```markdown
<!-- Skip playground link -->
```

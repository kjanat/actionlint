# generate-availability

This command generates [`availability.gen.go`](../../../model/contexts/availability.gen.go).

The command:

1. Fetches [the official contexts document](https://raw.githubusercontent.com/github/docs/refs/heads/main/content/actions/reference/workflows-and-actions/contexts.md).
2. Finds the "Context availability" table in the Markdown file.
3. Reads the contexts and special functions listed for each workflow key.
4. Generates the Go function and variable that actionlint uses to look up those restrictions.

## Background

GitHub Actions limits contexts and functions in certain places. For example:

- Only some workflow keys can access the `secrets` context.
- `jobs.<job_id>.if` and `jobs.<job_id>.steps.if` can use the `always()` function.

Actionlint checks these restrictions using a table of the contexts and special functions available at each workflow key.

## Usage

```sh
generate-availability [[srcfile] dstfile]
```

Generate the source from the repository root:

```sh
go run ./internal/development/generators/expression-contexts ./internal/model/contexts/availability.gen.go
```

To read a local copy of the documentation instead of downloading it, supply its
path relative to the current directory. The destination must also be a relative path:

```sh
go run ./internal/development/generators/expression-contexts path/to/contexts.md ./internal/model/contexts/availability.gen.go
```

Use `-` as the destination to print the generated source to stdout:

```sh
go run ./internal/development/generators/expression-contexts -
```

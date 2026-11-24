# expression-contexts generator

`run generate:contexts` reads the "Context availability" table in [GitHub's contexts document](https://raw.githubusercontent.com/github/docs/refs/heads/main/content/actions/reference/workflows-and-actions/contexts.md) on the `main` branch and regenerates [`availability.gen.go`](../../../model/contexts/availability.gen.go).

To read a local copy of the document, run this from the repository root. Both paths must be relative:

```sh
go run ./internal/development/generators/expression-contexts path/to/contexts.md ./internal/model/contexts/availability.gen.go
```

Without a destination, the generator prints to stdout.

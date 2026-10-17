package render_test

import (
	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/render"
	"os"
)

func ExampleFormatter() {
	// Errors returned from Linter methods
	errs := []diagmodel.Diagnostic{
		{
			Message: "error message 1",
			Path:    "foo.yaml",
			Start:   diagmodel.Position{Line: 1, Column: 4},
			Rule:    "rule1",
		},
		{
			Message: "error message 2",
			Path:    "foo.yaml",
			Start:   diagmodel.Position{Line: 3, Column: 1},
			Rule:    "rule2",
		},
	}

	// Create ErrorFormatter instance with template
	f, err := render.NewFormatter(`{{range $ := .}}{{$.Filepath}}:{{$.Line}}:{{$.Column}}: {{$.Message}}\n{{end}}`, func() string { return "example" }, nil)
	if err != nil {
		// Some error happened while creating the formatter (e.g. syntax error)
		panic(err)
	}

	src := `on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`

	// Prints all errors to stdout following the template
	var fields []*render.TemplateFields
	for _, d := range errs {
		fields = append(fields, render.FindingOf(d, []byte(src)).TemplateFields())
	}
	if err := f.Print(os.Stdout, fields); err != nil {
		panic(err)
	}

	// Output:
	// foo.yaml:1:4: error message 1
	// foo.yaml:3:1: error message 2
}

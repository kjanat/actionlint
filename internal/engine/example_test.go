package engine_test

import (
	"fmt"
	"os"
	"path/filepath"

	actionlint "actionlint.kjanat.dev/internal/engine"
)

func ExampleLinter() {
	// Specify linter options
	o := &actionlint.LinterOptions{
		IgnorePatterns: []string{`'label ".+" is unknown'`},
		// Other options...
	}

	// Create Linter instance which outputs errors to stdout
	l, err := actionlint.NewLinter(os.Stdout, o)
	if err != nil {
		panic(err)
	}

	// File to check
	f := filepath.Join("testdata", "examples", "main.yaml")

	// First return value is an array of lint errors found in the workflow files. The second return
	// value is an error of actionlint itself. This call outputs the lint errors to stdout. Use
	// io.Discard to prevent the output.
	//
	// There are several methods to run linter.
	// - LintFile: Check the given single file
	// - LintFiles: Check the given multiple files
	// - LintDir: Check all workflow files in the given single directory recursively
	// - LintRepository: Discover and check workflows and action manifests in the given repository
	// - LintStdin: Check the given workflow content read from STDIN
	// - Lint: Check the given workflow content assuming the given file path
	errs, err := l.LintFile(f, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(len(errs), "lint errors found by actionlint")
}

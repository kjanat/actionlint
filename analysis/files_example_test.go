package analysis_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/analysis"
)

func ExampleFiles() {
	directory, err := os.MkdirTemp("", "workflow-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "workflow.yml")
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		panic(err)
	}

	result, err := analysis.Files(context.Background(), []string{path}, analysis.FileOptions{
		IgnorePatterns: []string{`label ".+" is unknown`},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(result.Diagnostics), "lint errors found by actionlint")
	// Output: 0 lint errors found by actionlint
}

package application_test

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

func TestStdinSyntaxDiagnosticPath(t *testing.T) {
	for _, f := range []string{"", "foo.yaml"} {
		in := []byte(`on: push
jobs:
  job:
    runs-on: foo
	steps:
	  - run: echo`)
		result, err := application.Check(t.Context(), application.Options{WorkingDir: t.TempDir()}, application.Input{Kind: application.Stdin, Name: f, Reader: bytes.NewReader(in)})
		if err != nil {
			t.Fatalf("linting input with stdin file name %q caused error: %v", f, err)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("unexpected number of errors with stdin file name %q: %v", f, result.Diagnostics)
		}

		want := f
		if want == "" {
			want = "<stdin>"
		}
		if result.Diagnostics[0].Path != want {
			t.Fatalf("file path in the error with stdin file name %q should be %q but got %q", f, want, result.Diagnostics[0].Path)
		}
	}
}

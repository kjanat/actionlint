package application_test

import (
	"bytes"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

func TestStdinFlowMappingCompatibility(t *testing.T) {
	for _, separator := range []string{":,", ": ,", ": null,"} {
		t.Run(separator, func(t *testing.T) {
			source := "name: autofix.ci\nrun-name: test\non: { pull_request" + separator + " push: { branches: [master] } }\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := application.Check(t.Context(), application.Options{WorkingDir: t.TempDir()}, application.Input{
				Kind: application.Stdin, Name: "autofix.yml", Reader: strings.NewReader(source),
			})
			if err != nil {
				t.Fatal(err)
			}
			if separator != ":," {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("valid empty event rejected: %v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one diagnostic, got %v", result.Diagnostics)
			}
			d := result.Diagnostics[0]
			if d.Path != "autofix.yml" || d.Rule != "syntax-check" || d.Start.Line != 3 || d.Start.Column != 7 {
				t.Fatalf("unexpected diagnostic: %+v", d)
			}
		})
	}
}

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

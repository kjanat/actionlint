package checking_test

import (
	"bytes"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestStdinFlowMappingCompatibility(t *testing.T) {
	t.Parallel()
	for _, separator := range []string{":,", ": ,", ": null,"} {
		t.Run(separator, func(t *testing.T) {
			t.Parallel()
			source := "name: autofix.ci\nrun-name: test\non: { pull_request" + separator + " push: { branches: [master] } }\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			source += "permissions: {}\n"
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir()}, inputmodel.Request{
				Kind: inputmodel.Stdin, Name: "autofix.yml", Reader: strings.NewReader(source),
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
	t.Parallel()
	for _, f := range []string{"", "foo.yaml"} {
		in := []byte(`on: push
jobs:
  job:
    runs-on: foo
	steps:
	  - run: echo`)
		result, err := checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir()}, inputmodel.Request{Kind: inputmodel.Stdin, Name: f, Reader: bytes.NewReader(in)})
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

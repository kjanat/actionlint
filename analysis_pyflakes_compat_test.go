package actionlint_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestAnalysisDeprecatedPyflakesCompatibility(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing-pyflakes")
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(undefined_name)
`
	for _, tc := range []struct {
		name, command string
		options       *actionlint.ExternalCommandOptions
	}{
		{name: "omitted"},
		{name: "legacy command", command: "missing-pyflakes --invalid '"},
		{name: "missing executable", options: &actionlint.ExternalCommandOptions{Executable: &missing}},
		{name: "invalid arguments", command: missing, options: &actionlint.ExternalCommandOptions{Arguments: []string{"\x00"}}},
		{name: "invalid environment", command: missing, options: &actionlint.ExternalCommandOptions{Environment: []string{"invalid"}}},
		{name: "missing working directory", command: missing, options: &actionlint.ExternalCommandOptions{WorkingDir: filepath.Join(root, "missing-directory")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertResult := func(result *actionlint.AnalysisResult, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				if result == nil || result.FileCount() != 1 || len(result.Diagnostics) != 0 {
					t.Fatalf("retired checker changed analysis: %#v", result)
				}
			}
			assertResult(actionlint.Analyze(context.Background(), actionlint.AnalysisRequest{
				Sources:    []actionlint.SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow)}},
				WorkingDir: root, Pyflakes: tc.command, PyflakesOptions: tc.options,
			}))
			session, err := actionlint.NewAnalysisSession(actionlint.AnalysisOptions{
				WorkingDir: root, SkipProjectConfig: true,
				Pyflakes: tc.command, PyflakesOptions: tc.options,
			})
			if err != nil {
				t.Fatal(err)
			}
			required, err := session.RequiredTools(nil)
			if err != nil || required.Pyflakes {
				t.Fatalf("retired checker was required: %+v, %v", required, err)
			}
			assertResult(session.ReadStdin(strings.NewReader(workflow), false))
		})
	}
}

func TestAnalysisDeprecatedPyflakesFieldOrder(t *testing.T) {
	for _, tc := range []struct {
		typeOf            reflect.Type
		first, shellcheck string
	}{
		{reflect.TypeFor[actionlint.AnalysisOptions](), "Context", "Shellcheck"},
		{reflect.TypeFor[actionlint.AnalysisRequest](), "Sources", "ShellCheck"},
	} {
		t.Run(tc.typeOf.Name(), func(t *testing.T) {
			if got := tc.typeOf.Field(0).Name; got != tc.first {
				t.Fatalf("first field = %s, want %s", got, tc.first)
			}
			for _, pair := range [][2]string{{tc.shellcheck, "Pyflakes"}, {"ShellcheckSettings", "PyflakesOptions"}} {
				previous, ok := tc.typeOf.FieldByName(pair[0])
				if !ok || previous.Index[0]+1 >= tc.typeOf.NumField() || tc.typeOf.Field(previous.Index[0]+1).Name != pair[1] {
					t.Fatalf("%s must follow %s", pair[1], pair[0])
				}
			}
		})
	}
	typeOf := reflect.TypeFor[actionlint.ExternalToolRequirements]()
	field, ok := typeOf.FieldByName("Pyflakes")
	if !ok || field.Index[0] != 1 || field.Type.Kind() != reflect.Bool || field.Tag.Get("json") != "pyflakes" {
		t.Fatalf("retired tool requirement contract changed: %+v", field)
	}
}

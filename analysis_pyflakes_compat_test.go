package actionlint_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestAnalysisDeprecatedPyflakesCompatibility(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing-pyflakes")
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(undefined_name)\n"
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
			assertResult(session.ReadStdin(strings.NewReader(workflow), false))
		})
	}
}

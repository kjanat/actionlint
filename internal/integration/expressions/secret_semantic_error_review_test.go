package expressions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestSecretInterpolationIndependentSemanticFindings(t *testing.T) {
	for _, interpolation := range []string{"on", "off"} {
		for _, tc := range []struct {
			expression  string
			wantSecrets int
		}{
			{"secrets.TOKEN || github.events", 1},
			{"github.events || secrets.TOKEN", 1},
			{"github.token.invalid || secrets.TOKEN", 1},
			{"secrets[true]", 0},
			{"secrets.TOKEN.invalid", 0},
		} {
			t.Run(interpolation+"/"+tc.expression, func(t *testing.T) {
				cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}\nlint: {rules: {correctness: {script-interpolation: " + interpolation + "}, security: {script-injection: on}}}"))
				if err != nil {
					t.Fatal(err)
				}
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + tc.expression + " }}\"\n"
				result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(source), Config: cfg}})
				if err != nil {
					t.Fatal(err)
				}
				secrets, semantic := 0, 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule == "script-injection" {
						secrets++
					} else {
						semantic++
					}
				}
				if secrets != tc.wantSecrets || semantic != 1 {
					t.Fatalf("got %d secret findings and %d semantic findings, want %d and 1: %+v", secrets, semantic, tc.wantSecrets, result.Diagnostics)
				}
			})
		}
	}
}

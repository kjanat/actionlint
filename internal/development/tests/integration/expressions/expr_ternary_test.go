package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestUnsoundTernaryConfigurationAndSuppression(t *testing.T) {
	t.Parallel()
	src := `on: push
env:
  VALUE: ${{ github.ref && '' || 'fallback' }}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`
	for _, config := range []string{"", "lint: {rules: {correctness: {unsound-ternary: off}}}", "lint: {rules: {correctness: {unsound-ternary: warn}}}"} {
		cfg, err := checking.ConfigurationRules().Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src), Config: cfg}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(config, "off") {
			if len(result.Diagnostics) != 0 {
				t.Fatal(result.Diagnostics)
			}
			continue
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "unsound-ternary" {
			t.Fatal(result.Diagnostics)
		}
		if strings.Contains(config, "warn") && result.Diagnostics[0].Severity != "warning" {
			t.Fatal(result.Diagnostics)
		}
	}
	src = strings.Replace(src, "|| 'fallback' }}", "|| 'fallback' }} # actionlint:ignore unsound-ternary -- testing fallback behavior", 1)
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v, %v", result, err)
	}
}

package actionlint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataSuppressionAliasConfigurations(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
	writeShellcheckFixture(t, root, "inner/action.yml", `name: inner # actionlint:ignore action
description: test
runs:
  using: node24
  main: index.js
`)
	if err := os.Symlink(filepath.Join(root, "inner"), filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", `overrides:
  - includes: ['inner/**']
    lint: {rules: {correctness: {inline-suppression: warn}}}
  - includes: ['alias/**']
    lint: {rules: {correctness: {inline-suppression: info}}}
`)
	result := compositeAnalysis(t, root, "- uses: ./inner\n- uses: ./alias", AnalysisOptions{})
	counts := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule != "inline-suppression" {
			t.Fatalf("unexpected finding: %+v", diagnostic)
		}
		counts[diagnostic.Severity]++
	}
	if len(result.Diagnostics) != 2 || counts["warning"] != 1 || counts["info"] != 1 {
		t.Fatalf("alias scopes collapsed: %+v", result.Diagnostics)
	}
}

func TestMetadataSuppressionIgnoresWorkflowOptions(t *testing.T) {
	for _, runtime := range []string{"using: node24\n  main: index.js", "using: docker\n  image: docker://alpine:3.22", `using: composite
  steps:
    - shell: bash
      run: echo ok`} {
		for _, directive := range []string{"# actionlint:ignore action", "# actionlint:ignore action -- reviewed"} {
			t.Run(runtime+directive, func(t *testing.T) {
				root, _ := executableFixture(t)
				config := `lint:
  rules:
    policy:
      require-job-timeout: {level: on, options: {min-minutes: 5}}
overrides:
  - includes: ['inner/**']
    lint:
      rules:
        correctness: {inline-suppression: warn}
        policy:
          require-job-timeout: {level: on, options: {max-minutes: 3}}
          disallow-suppressions: {level: warn, options: {report: suppression}}
`
				writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
				writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
				writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+directive+`
description: test
runs:
  `+runtime+"\n")
				result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
				count := 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule == "inline-suppression" || diagnostic.Rule == "disallow-suppressions" {
						count++
						if diagnostic.Path != filepath.Join("inner", "action.yml") || diagnostic.Severity != "warning" {
							t.Fatalf("metadata settings lost: %+v", diagnostic)
						}
					}
				}
				if count != 1 {
					t.Fatalf("want one metadata warning: %+v", result.Diagnostics)
				}
			})
		}
	}
}

package actionlint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataSuppressionAliasConfigurations(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner # actionlint:ignore action\ndescription: test\nruns:\n  using: node24\n  main: index.js\n")
	if err := os.Symlink(filepath.Join(root, "inner"), filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "overrides:\n  - includes: ['inner/**']\n    lint: {rules: {correctness: {inline-suppression: warn}}}\n  - includes: ['alias/**']\n    lint: {rules: {correctness: {inline-suppression: info}}}\n")
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
	for _, runtime := range []string{"using: node24\n  main: index.js", "using: docker\n  image: docker://alpine:3.22", "using: composite\n  steps:\n    - shell: bash\n      run: echo ok"} {
		for _, directive := range []string{"# actionlint:ignore action", "# actionlint:ignore action -- reviewed"} {
			t.Run(runtime+directive, func(t *testing.T) {
				root, _ := executableFixture(t)
				config := "lint:\n  rules:\n    policy:\n      require-job-timeout: {level: on, options: {min-minutes: 5}}\noverrides:\n  - includes: ['inner/**']\n    lint:\n      rules:\n        correctness: {inline-suppression: warn}\n        policy:\n          require-job-timeout: {level: on, options: {max-minutes: 3}}\n          disallow-suppressions: {level: warn, options: {report: suppression}}\n"
				writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
				writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
				writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+directive+"\ndescription: test\nruns:\n  "+runtime+"\n")
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

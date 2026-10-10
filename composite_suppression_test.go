package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalActionSuppressionDiagnosticLevels(t *testing.T) {
	for _, runtime := range []struct{ name, runs string }{
		{"javascript", "using: node24\n  main: index.js"},
		{"docker", "using: docker\n  image: docker://alpine:3.22"},
		{"composite", "using: composite\n  steps:\n    - shell: bash\n      run: echo ok"},
	} {
		for _, tc := range []struct {
			name, rule, group, directive, caller, action string
			wantCaller, wantAction                       string
		}{
			{"malformed-action-off", "inline-suppression", "correctness", "# actionlint:ignore action", "error", "off", "error", ""},
			{"malformed-caller-off", "inline-suppression", "correctness", "# actionlint:ignore action", "off", "error", "", "error"},
			{"malformed-severities", "inline-suppression", "correctness", "# actionlint:ignore action", "info", "warn", "info", "warning"},
			{"policy-action-only", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "off", "error", "", "error"},
			{"policy-caller-only", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "error", "off", "error", ""},
			{"policy-severities", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "info", "warn", "info", "warning"},
		} {
			t.Run(runtime.name+"/"+tc.name, func(t *testing.T) {
				root, _ := executableFixture(t)
				config := fmt.Sprintf("lint: {rules: {%s: {%s: %s}}}\noverrides:\n  - includes: ['inner/action.yml']\n    lint: {rules: {%s: {%s: %s}}}\n", tc.group, tc.rule, tc.caller, tc.group, tc.rule, tc.action)
				writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
				writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
				writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+"\ndescription: test\nruns:\n  "+runtime.runs+"\n")
				result := compositeAnalysis(t, root, "- uses: ./inner "+tc.directive, AnalysisOptions{})
				want := make(map[string]string)
				if tc.wantCaller != "" {
					want[filepath.Join(".github", "workflows", "composite.yml")] = tc.wantCaller
				}
				if tc.wantAction != "" {
					want[filepath.Join("inner", "action.yml")] = tc.wantAction
				}
				if len(result.Diagnostics) != len(want) {
					t.Fatalf("want %v, got %+v", want, result.Diagnostics)
				}
				for _, diagnostic := range result.Diagnostics {
					if severity, exists := want[diagnostic.Path]; !exists || diagnostic.Rule != tc.rule || diagnostic.Severity != severity {
						t.Fatalf("want %v for %s, got %+v", want, tc.rule, diagnostic)
					}
				}
			})
		}
	}
}

func TestLocalActionSuppressionsRespectCallerLintSwitch(t *testing.T) {
	for _, runtime := range []struct{ name, runs string }{
		{"javascript", "using: node24\n  main: index.js"},
		{"docker", "using: docker\n  image: docker://alpine:3.22"},
		{"composite", "using: composite\n  steps:\n    - shell: bash\n      run: echo ok"},
	} {
		for _, tc := range []struct{ name, directive, config, rule string }{
			{"malformed", "# actionlint:ignore action", "", "inline-suppression"},
			{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
		} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/enabled=%t", runtime.name, tc.name, enabled), func(t *testing.T) {
					root, _ := executableFixture(t)
					config := tc.config + fmt.Sprintf("overrides:\n  - includes: ['.github/workflows/**']\n    lint: {enabled: %t}\n", enabled)
					writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
					writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
					writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+"\ndescription: test\nruns:\n  "+runtime.runs+"\n")
					result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
					if !enabled {
						if len(result.Diagnostics) != 0 {
							t.Fatalf("disabled caller emitted diagnostics: %+v", result.Diagnostics)
						}
					} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.rule || filepath.ToSlash(result.Diagnostics[0].Path) != "inner/action.yml" {
						t.Fatalf("enabled caller lost %s in local action: %+v", tc.rule, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestCleanCompositeSuppressionDirectives(t *testing.T) {
	for _, tc := range []struct {
		name, directive, config, want string
	}{
		{"malformed", "# actionlint:ignore shellcheck", "", "inline-suppression"},
		{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
		{"allowed", "# actionlint:ignore shellcheck -- reviewed", "", ""},
		{"override", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\noverrides:\n  - includes: ['inner/action.yml']\n    lint: {rules: {policy: {disallow-suppressions: off}}}\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", tc.config)
			writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
			metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+"\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
			result := compositeAnalysis(t, root, "- uses: ./outer\n- uses: ./outer", AnalysisOptions{})
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.want || result.Diagnostics[0].Start.Line != 1 || filepath.Join(root, result.Diagnostics[0].Path) != metadata {
				t.Fatalf("want one %s in inner/action.yml:1: %+v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeBlockSuppression(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: | # actionlint:ignore shellcheck -- reviewed expansion\n        echo $VALUE\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
}

func TestCleanLocalActionSuppressionDirectives(t *testing.T) {
	for _, runtime := range []struct{ name, runs string }{
		{"javascript", "using: node24\n  main: index.js"},
		{"docker", "using: docker\n  image: docker://alpine:3.22"},
	} {
		for _, invocation := range []struct {
			name   string
			nested bool
		}{{"direct", false}, {"nested", true}} {
			for _, tc := range []struct{ name, directive, config, want string }{
				{"malformed", "# actionlint:ignore action", "", "inline-suppression"},
				{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
				{"allowed", "# actionlint:ignore action -- reviewed", "", ""},
				{"override", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\noverrides:\n  - includes: ['inner/action.yml']\n    lint: {rules: {policy: {disallow-suppressions: off}}}\n", ""},
			} {
				t.Run(runtime.name+"/"+invocation.name+"/"+tc.name, func(t *testing.T) {
					root, _ := executableFixture(t)
					writeShellcheckFixture(t, root, ".github/actionlint.yaml", tc.config)
					writeShellcheckFixture(t, root, "inner/index.js", "console.log('ok');\n")
					metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+"\ndescription: test\nruns:\n  "+runtime.runs+"\n")
					steps := "- uses: ./inner\n- uses: ./inner"
					if invocation.nested {
						writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
						steps = "- uses: ./outer\n- uses: ./outer"
					}
					result := compositeAnalysis(t, root, steps, AnalysisOptions{})
					if tc.want == "" {
						if len(result.Diagnostics) != 0 {
							t.Fatal(result.Diagnostics)
						}
						return
					}
					if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.want || result.Diagnostics[0].Start.Line != 1 || filepath.Join(root, result.Diagnostics[0].Path) != metadata {
						t.Fatalf("want one %s in inner/action.yml:1: %+v", tc.want, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestLocalActionSuppressionSourcesStayWorkflowScoped(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/index.js", "console.log('ok');\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: local # actionlint:ignore action\ndescription: test\nruns:\n  using: node24\n  main: index.js\n")
	used := writeShellcheckFixture(t, root, ".github/workflows/used.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n")
	unused := writeShellcheckFixture(t, root, ".github/workflows/unused.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
	project := &Project{root: root}
	actions := NewLocalActionsCache(project, nil)
	workflows := NewLocalReusableWorkflowCache(project, root, nil)
	process := newConcurrentProcess(t.Context(), 1)
	engine := &analysisEngine{ctx: t.Context(), workingDir: root, inputs: &inputFiles{}, gitModes: &gitModes{}}
	for _, workflow := range []string{used, unused, used} {
		source, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		var rules []Rule
		findings, err := engine.check(workflow, source, project, nil, process, actions, workflows, &rules)
		if err != nil {
			t.Fatal(err)
		}
		if workflow == unused {
			if len(findings) != 0 {
				t.Fatalf("unreferenced metadata leaked into workflow: %+v", findings)
			}
		} else if len(findings) != 1 || findings[0].Kind != "inline-suppression" {
			t.Fatalf("metadata directive lost on repeated analysis: %+v", findings)
		}
	}
}

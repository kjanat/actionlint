package suppression_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestInvalidLocalActionSuppressionDirectives(t *testing.T) {
	for _, metadata := range []struct {
		name, source string
	}{
		{"steps-mapping", `name: inner %s
runs: {using: composite, steps: {}}
`},
		{"false-inputs", `name: inner %s
inputs: false
runs: {using: node24, main: index.js}
`},
		{"false-runs", `name: inner %s
runs: false
`},
		{"false-document", "false %s\n"},
		{"invalid-yaml", "name: [ %s\n"},
	} {
		for _, nested := range []bool{false, true} {
			for _, tc := range []struct{ name, directive, config, want string }{
				{"malformed", "# actionlint:ignore action --", "", "inline-suppression"},
				{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
				{"allowed", "# actionlint:ignore action -- reviewed", "", ""},
			} {
				t.Run(fmt.Sprintf("%s/nested=%t/%s", metadata.name, nested, tc.name), func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", tc.config)
					testutil.WriteFixture(t, root, "inner/action.yml", fmt.Sprintf(metadata.source, tc.directive))
					steps := "- uses: ./inner\n- uses: ./inner"
					if nested {
						testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
						steps = "- uses: ./outer\n- uses: ./outer"
					}
					result := testutil.Composite(t, root, steps, application.Options{})
					var syntaxCount, directiveCount int
					for _, diagnostic := range result.Diagnostics {
						switch {
						case diagnostic.Rule == "action" && strings.Contains(diagnostic.Message, "could not parse action metadata"):
							syntaxCount++
						case tc.want != "" && diagnostic.Rule == tc.want && filepath.ToSlash(diagnostic.Path) == "inner/action.yml" && diagnostic.Start.Line == 1:
							directiveCount++
						default:
							t.Fatalf("unexpected diagnostic: %+v", diagnostic)
						}
					}
					wantDirective := 0
					if tc.want != "" {
						wantDirective = 1
					}
					if syntaxCount != 1 || directiveCount != wantDirective {
						t.Fatalf("want one metadata error and %d directive errors, got %+v", wantDirective, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestLocalActionSuppressionDiagnosticLevels(t *testing.T) {
	for _, runtime := range []struct{ name, runs string }{
		{"javascript", "using: node24\n  main: index.js"},
		{"docker", "using: docker\n  image: docker://alpine:3.22"},
		{"composite", `using: composite
  steps:
    - shell: bash
      run: echo ok`},
	} {
		for _, tc := range []struct {
			name, rule, group, directive, caller, action string
			wantCaller, wantAction                       string
		}{
			{"malformed-action-off", "inline-suppression", "correctness", "# actionlint:ignore action --", "error", "off", "error", ""},
			{"malformed-caller-off", "inline-suppression", "correctness", "# actionlint:ignore action --", "off", "error", "", "error"},
			{"malformed-severities", "inline-suppression", "correctness", "# actionlint:ignore action --", "info", "warn", "info", "warning"},
			{"policy-action-only", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "off", "error", "", "error"},
			{"policy-caller-only", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "error", "off", "error", ""},
			{"policy-severities", "disallow-suppressions", "policy", "# actionlint:ignore action -- reviewed", "info", "warn", "info", "warning"},
		} {
			t.Run(runtime.name+"/"+tc.name, func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				config := fmt.Sprintf(`lint: {rules: {%s: {%s: %s}}}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {%s: {%s: %s}}}
`, tc.group, tc.rule, tc.caller, tc.group, tc.rule, tc.action)
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
				testutil.WriteFixture(t, root, "inner/index.js", "console.log('ok');\n")
				testutil.WriteFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
				result := testutil.Composite(t, root, "- uses: ./inner "+tc.directive, application.Options{})
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
		{"composite", `using: composite
  steps:
    - shell: bash
      run: echo ok`},
	} {
		for _, tc := range []struct{ name, directive, config, rule string }{
			{"malformed", "# actionlint:ignore action --", "", "inline-suppression"},
			{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
		} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/enabled=%t", runtime.name, tc.name, enabled), func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					config := tc.config + fmt.Sprintf(`overrides:
  - includes: ['.github/workflows/**']
    lint: {enabled: %t}
`, enabled)
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
					testutil.WriteFixture(t, root, "inner/index.js", "console.log('ok');\n")
					testutil.WriteFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
					result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
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
		{"malformed", "# actionlint:ignore shellcheck --", "", "inline-suppression"},
		{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
		{"allowed", "# actionlint:ignore shellcheck -- reviewed", "", ""},
		{"override", "# actionlint:ignore shellcheck -- reviewed", `policy: {disallow-suppressions: true}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {policy: {disallow-suppressions: off}}}
`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", tc.config)
			testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
			metadata := testutil.WriteFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
			result := testutil.Composite(t, root, "- uses: ./outer\n- uses: ./outer", application.Options{})
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
	command := testutil.RequireTool(t, "shellcheck")
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: | # actionlint:ignore shellcheck -- reviewed expansion
        echo $VALUE
`)
	result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
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
				{"malformed", "# actionlint:ignore action --", "", "inline-suppression"},
				{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
				{"allowed", "# actionlint:ignore action -- reviewed", "", ""},
				{"override", "# actionlint:ignore action -- reviewed", `policy: {disallow-suppressions: true}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {policy: {disallow-suppressions: off}}}
`, ""},
			} {
				t.Run(runtime.name+"/"+invocation.name+"/"+tc.name, func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", tc.config)
					testutil.WriteFixture(t, root, "inner/index.js", "console.log('ok');\n")
					metadata := testutil.WriteFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
					steps := "- uses: ./inner\n- uses: ./inner"
					if invocation.nested {
						testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
						steps = "- uses: ./outer\n- uses: ./outer"
					}
					result := testutil.Composite(t, root, steps, application.Options{})
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

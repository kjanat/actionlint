package suppression_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
)

func TestInvalidLocalActionSuppressionDirectives(t *testing.T) {
	t.Parallel()
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
				{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "forbidden-suppression"},
				{"allowed", "# actionlint:ignore action -- reviewed", "", ""},
			} {
				t.Run(fmt.Sprintf("%s/nested=%t/%s", metadata.name, nested, tc.name), func(t *testing.T) {
					t.Parallel()
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.config)
					fixturefiles.Write(t, root, "inner/action.yml", fmt.Sprintf(metadata.source, tc.directive))
					steps := "- uses: ./inner\n- uses: ./inner"
					if nested {
						fixturefiles.Write(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
						steps = "- uses: ./outer\n- uses: ./outer"
					}
					result := testutil.Composite(t, root, steps, checking.Options{})
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
	t.Parallel()
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
			{"policy-action-only", "forbidden-suppression", "policy", "# actionlint:ignore action -- reviewed", "off", "error", "", "error"},
			{"policy-caller-only", "forbidden-suppression", "policy", "# actionlint:ignore action -- reviewed", "error", "off", "error", ""},
			{"policy-severities", "forbidden-suppression", "policy", "# actionlint:ignore action -- reviewed", "info", "warn", "info", "warning"},
		} {
			t.Run(runtime.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				config := fmt.Sprintf(`lint: {rules: {%s: {%s: %s}}}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {%s: {%s: %s}}}
`, tc.group, tc.rule, tc.caller, tc.group, tc.rule, tc.action)
				fixturefiles.Write(t, root, ".github/actionlint.yaml", config)
				fixturefiles.Write(t, root, "inner/index.js", "console.log('ok');\n")
				fixturefiles.Write(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
				result := testutil.Composite(t, root, "- uses: ./inner "+tc.directive, checking.Options{})
				type report struct{ path, rule string }
				want := make(map[report]string)
				if tc.wantCaller != "" {
					want[report{filepath.Join(".github", "workflows", "composite.yml"), tc.rule}] = tc.wantCaller
				}
				if tc.wantAction != "" {
					want[report{filepath.Join("inner", "action.yml"), tc.rule}] = tc.wantAction
				}
				if tc.rule == "forbidden-suppression" {
					want[report{filepath.Join(".github", "workflows", "composite.yml"), "unused-suppression"}] = "info"
					want[report{filepath.Join("inner", "action.yml"), "unused-suppression"}] = "info"
				}
				if len(result.Diagnostics) != len(want) {
					t.Fatalf("want %v, got %+v", want, result.Diagnostics)
				}
				for _, diagnostic := range result.Diagnostics {
					key := report{diagnostic.Path, diagnostic.Rule}
					if severity, exists := want[key]; !exists || diagnostic.Severity != severity {
						t.Fatalf("want %v for %s, got %+v", want, tc.rule, diagnostic)
					}
					delete(want, key)
				}
			})
		}
	}
}

func TestLocalActionSuppressionsRespectCallerLintSwitch(t *testing.T) {
	t.Parallel()
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
			{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", "forbidden-suppression"},
		} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/enabled=%t", runtime.name, tc.name, enabled), func(t *testing.T) {
					t.Parallel()
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					config := tc.config + fmt.Sprintf(`overrides:
  - includes: ['.github/workflows/**']
    lint: {enabled: %t}
`, enabled)
					fixturefiles.Write(t, root, ".github/actionlint.yaml", config)
					fixturefiles.Write(t, root, "inner/index.js", "console.log('ok');\n")
					fixturefiles.Write(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
					result := testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
					if !enabled {
						if len(result.Diagnostics) != 0 {
							t.Fatalf("disabled caller emitted diagnostics: %+v", result.Diagnostics)
						}
					} else {
						want := map[string]bool{tc.rule: true}
						if tc.name == "prohibited" {
							want["unused-suppression"] = true
						}
						if len(result.Diagnostics) != len(want) {
							t.Fatalf("enabled caller: want %v in local action, got %+v", want, result.Diagnostics)
						}
						for _, diagnostic := range result.Diagnostics {
							if !want[diagnostic.Rule] || filepath.ToSlash(diagnostic.Path) != "inner/action.yml" {
								t.Fatalf("enabled caller lost directive origin: %+v", diagnostic)
							}
							delete(want, diagnostic.Rule)
						}
					}
				})
			}
		}
	}
}

func TestCleanCompositeSuppressionDirectives(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, directive, config string
		want                    []string
	}{
		{"malformed", "# actionlint:ignore shellcheck --", "", []string{"inline-suppression"}},
		{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", []string{"unused-suppression", "forbidden-suppression"}},
		{"allowed", "# actionlint:ignore shellcheck -- reviewed", "", []string{"unused-suppression"}},
		{"override", "# actionlint:ignore shellcheck -- reviewed", `policy: {disallow-suppressions: true}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {policy: {disallow-suppressions: off}}}
`, []string{"unused-suppression"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.config)
			fixturefiles.Write(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
			metadata := fixturefiles.Write(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
			result := testutil.Composite(t, root, "- uses: ./outer\n- uses: ./outer", checking.Options{})
			if len(result.Diagnostics) != len(tc.want) {
				t.Fatalf("want %v in inner/action.yml:1: %+v", tc.want, result.Diagnostics)
			}
			for i, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != tc.want[i] || diagnostic.Start.Line != 1 || filepath.Join(root, diagnostic.Path) != metadata {
					t.Fatalf("want %s in inner/action.yml:1: %+v", tc.want[i], diagnostic)
				}
			}
		})
	}
}

func TestCompositeBlockSuppression(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true, ruff: false}\n")
	const metadata = `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: | # actionlint:ignore shellcheck -- reviewed expansion
        echo $VALUE
`
	fixturefiles.Write(t, root, "local/action.yml", metadata)
	result := testutil.Composite(t, root, "- uses: ./local", checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	fixturefiles.Write(t, root, "local/action.yml", strings.Replace(metadata, " # actionlint:ignore shellcheck -- reviewed expansion", "", 1))
	result = testutil.Composite(t, root, "- uses: ./local", checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("unsuppressed control did not run ShellCheck: %+v", result.Diagnostics)
	}
}

func TestCleanLocalActionSuppressionDirectives(t *testing.T) {
	t.Parallel()
	for _, runtime := range []struct{ name, runs string }{
		{"javascript", "using: node24\n  main: index.js"},
		{"docker", "using: docker\n  image: docker://alpine:3.22"},
	} {
		for _, invocation := range []struct {
			name   string
			nested bool
		}{{"direct", false}, {"nested", true}} {
			for _, tc := range []struct {
				name, directive, config string
				want                    []string
			}{
				{"malformed", "# actionlint:ignore action --", "", []string{"inline-suppression"}},
				{"prohibited", "# actionlint:ignore action -- reviewed", "policy: {disallow-suppressions: true}\n", []string{"unused-suppression", "forbidden-suppression"}},
				{"allowed", "# actionlint:ignore action -- reviewed", "", []string{"unused-suppression"}},
				{"override", "# actionlint:ignore action -- reviewed", `policy: {disallow-suppressions: true}
overrides:
  - includes: ['inner/action.yml']
    lint: {rules: {policy: {disallow-suppressions: off}}}
`, []string{"unused-suppression"}},
			} {
				t.Run(runtime.name+"/"+invocation.name+"/"+tc.name, func(t *testing.T) {
					t.Parallel()
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.config)
					fixturefiles.Write(t, root, "inner/index.js", "console.log('ok');\n")
					metadata := fixturefiles.Write(t, root, "inner/action.yml", "name: inner "+tc.directive+`
description: test
runs:
  `+runtime.runs+"\n")
					steps := "- uses: ./inner\n- uses: ./inner"
					if invocation.nested {
						fixturefiles.Write(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
						steps = "- uses: ./outer\n- uses: ./outer"
					}
					result := testutil.Composite(t, root, steps, checking.Options{})
					if len(result.Diagnostics) != len(tc.want) {
						t.Fatalf("want %v in inner/action.yml:1: %+v", tc.want, result.Diagnostics)
					}
					for i, diagnostic := range result.Diagnostics {
						if diagnostic.Rule != tc.want[i] || diagnostic.Start.Line != 1 || filepath.Join(root, diagnostic.Path) != metadata {
							t.Fatalf("want %s in inner/action.yml:1: %+v", tc.want[i], diagnostic)
						}
					}
				})
			}
		}
	}
}

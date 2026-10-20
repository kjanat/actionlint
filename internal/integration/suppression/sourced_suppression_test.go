package suppression_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestSourcedShellcheckDeclarationSuppression(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	for _, composite := range []bool{false, true} {
		for _, tc := range []struct {
			name, declaration, config, directive string
			keep                                 bool
		}{
			{"inline", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "", "", false},
			{"block", "run: | # actionlint:ignore shellcheck -- reviewed\n  . ./scripts/check.sh", "", "", false},
			{"next-line", "# actionlint:ignore-next-line shellcheck -- reviewed\nrun: . ./scripts/check.sh", "", "", false},
			{"split-header", `run: !!str
  | # actionlint:ignore shellcheck -- reviewed
  . ./scripts/check.sh`, "", "", false},
			{"split-key", `run: # actionlint:ignore shellcheck -- reviewed
  !!str
  |
  . ./scripts/check.sh`, "", "", false},
			{"split-anchor", `run:
  !!str
  &script # actionlint:ignore shellcheck -- reviewed
  |
  . ./scripts/check.sh
name: *script`, "", "", false},
			{"optional-reason", "run: . ./scripts/check.sh # actionlint:ignore shellcheck", "", "", false},
			{"empty-reason", "run: . ./scripts/check.sh # actionlint:ignore shellcheck --", "", "inline-suppression", true},
			{"forbidden", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions", true},
			{"report-suppression", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: {report: suppression}}\n", "disallow-suppressions", false},
			{"report-violation", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: {report: violation}}\n", "", true},
			{"other-declaration", "run: . ./scripts/check.sh\nenv: {OTHER: value} # actionlint:ignore shellcheck -- reviewed", "", "", true},
		} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("composite=%t/%s/%q", composite, tc.name, ending), func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t)
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n"+tc.config)
					script := testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
					script, err := filepath.EvalSymlinks(script)
					if err != nil {
						t.Fatal(err)
					}
					declaration := strings.ReplaceAll(tc.declaration, "\n", ending)
					steps := `- shell: bash
  working-directory: .
  ` + strings.ReplaceAll(declaration, "\n", "\n  ")
					origin := ".github/workflows/composite.yml"
					if composite {
						origin = "local/action.yml"
						testutil.WriteFixture(t, root, origin, `name: local
description: test
runs:
  using: composite
  steps:
    `+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
						steps = "- uses: ./local"
					}
					result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()})
					counts := map[string]int{}
					for _, diagnostic := range result.Diagnostics {
						counts[diagnostic.Rule]++
						if diagnostic.Rule == "shellcheck" {
							if filepath.ToSlash(diagnostic.Path) != "scripts/check.sh" || diagnostic.Code != "SC2086" || diagnostic.Severity != "info" || diagnostic.Start != (diagmodel.Position{Line: 1, Column: 6}) || diagnostic.End != (diagmodel.Position{Line: 1, Column: 12}) || diagnostic.Snippet != "echo $VALUE" {
								t.Fatalf("source diagnostic was changed: %+v", diagnostic)
							}
						} else if diagnostic.Rule != tc.directive || filepath.ToSlash(diagnostic.Path) != origin {
							t.Fatalf("directive diagnostic lost origin: %+v", diagnostic)
						}
					}
					want := 0
					if tc.keep {
						want++
						if counts["shellcheck"] != 1 {
							t.Fatalf("source finding missing: %+v", result.Diagnostics)
						}
					}
					if tc.directive != "" {
						want++
						if counts[tc.directive] != 1 {
							t.Fatalf("directive finding missing: %+v", result.Diagnostics)
						}
					}
					if len(result.Diagnostics) != want || !slices.Contains(result.Inputs, script) {
						t.Fatalf("want %d findings and script input, got %+v, %v", want, result.Diagnostics, result.Inputs)
					}
				})
			}
		}
	}
}

func TestSourcedShellcheckOriginIsolation(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	const directive = " # actionlint:ignore shellcheck -- reviewed"
	for _, composite := range []bool{false, true} {
		for _, both := range []bool{false, true} {
			t.Run(fmt.Sprintf("composite=%t/both=%t", composite, both), func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t)
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
				testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				step := `- shell: bash
  working-directory: .
  run: . ./scripts/check.sh`
				steps := step + directive + "\n" + step
				if both {
					steps += directive
				}
				if composite {
					testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    `+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
					testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./local
`)
					steps = "- uses: ./outer"
				}
				result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()})
				want := 1
				if both {
					want = 0
				}
				if len(result.Diagnostics) != want {
					t.Fatalf("suppression crossed run origins: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestSourcedShellcheckAliasOrigin(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
	steps := `- shell: bash
  env: {SCRIPT: &script '. ./scripts/check.sh'}
  run: echo safe
- shell: bash
  run: echo safe # actionlint:ignore shellcheck -- other run
- shell: bash
  working-directory: .
  run: *script`
	result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()})
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("alias origin crossed intervening declarations: %+v", result.Diagnostics)
	}
}

func TestSourcedShellcheckMetadataScope(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	for _, ignoredPath := range []string{"", "scripts/**", "local/**"} {
		t.Run(ignoredPath, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			config := `tools: {shellcheck: true}
policy: {disallow-suppressions: true}
lint: {rules: {policy: {require-job-timeout: {level: on, options: {min-minutes: 5}}}}}
overrides:
  - includes: ['local/**']
    lint: {rules: {policy: {require-job-timeout: {level: on, options: {max-minutes: 3}}, disallow-suppressions: off}}}
  - includes: ['alias/**']
    lint: {rules: {policy: {disallow-suppressions: warn}}}
`
			if ignoredPath != "" {
				config += "paths:\n  '" + ignoredPath + `':
    ignore: ['SC2086']
`
			}
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
			testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
			testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: .
      run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed
`)
			if err := os.Symlink(filepath.Join(root, "local"), filepath.Join(root, "alias")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			result := testutil.Composite(t, root, "- uses: ./local\n- uses: ./alias", application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()})
			var sourced, policy int
			for _, diagnostic := range result.Diagnostics {
				switch diagnostic.Rule {
				case "shellcheck":
					sourced++
					if filepath.ToSlash(diagnostic.Path) != "scripts/check.sh" || diagnostic.Snippet != "echo $VALUE" || diagnostic.Code != "SC2086" {
						t.Fatalf("sourced diagnostic changed: %+v", diagnostic)
					}
				case "disallow-suppressions":
					policy++
					if diagnostic.Severity != "warning" {
						t.Fatalf("alias policy lost severity: %+v", diagnostic)
					}
				case "require-job-timeout":
				default:
					t.Fatalf("unexpected finding: %+v", diagnostic)
				}
			}
			want := 1
			if ignoredPath == "scripts/**" {
				want = 0
			}
			if sourced != want || policy != 1 {
				t.Fatalf("metadata policy or actual-script path filter lost: %+v", result.Diagnostics)
			}
		})
	}
}

func TestSourcedScriptsAreNotSuppressionMetadata(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	for _, suffix := range []string{".sh", ".yml", ".yaml"} {
		for _, composite := range []bool{false, true} {
			for _, tc := range []struct{ name, directive, policy string }{
				{"valid", "# actionlint:ignore shellcheck -- reviewed", ""},
				{"malformed", "# actionlint:ignore shellcheck", ""},
				{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n"},
			} {
				t.Run(fmt.Sprintf("%s/composite=%t/%s", suffix, composite, tc.name), func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t)
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n"+tc.policy)
					name := "scripts/check" + suffix
					testutil.WriteFixture(t, root, name, "echo $VALUE "+tc.directive+"\n")
					steps := "- run: . ./" + name + `
  shell: bash
  working-directory: .`
					if composite {
						testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: . ./`+name+"\n")
						steps = "- uses: ./local"
					}
					result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()})
					if len(result.Diagnostics) != 1 {
						t.Fatalf("sourced script comment changed diagnostics: %+v", result.Diagnostics)
					}
					finding := result.Diagnostics[0]
					if finding.Rule != "shellcheck" || finding.Code != "SC2086" || filepath.ToSlash(finding.Path) != name || finding.Start != (diagmodel.Position{Line: 1, Column: 6}) {
						t.Fatalf("sourced finding lost or replaced: %+v", finding)
					}
				})
			}
		}
	}
}

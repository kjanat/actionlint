package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSourcedShellcheckDeclarationSuppression(t *testing.T) {
	command := shellcheckForTest(t)
	for _, composite := range []bool{false, true} {
		for _, tc := range []struct {
			name, declaration, config, directive string
			keep                                 bool
		}{
			{"inline", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "", "", false},
			{"block", "run: | # actionlint:ignore shellcheck -- reviewed\n  . ./scripts/check.sh", "", "", false},
			{"next-line", "# actionlint:ignore-next-line shellcheck -- reviewed\nrun: . ./scripts/check.sh", "", "", false},
			{"split-header", "run: !!str\n  | # actionlint:ignore shellcheck -- reviewed\n  . ./scripts/check.sh", "", "", false},
			{"split-key", "run: # actionlint:ignore shellcheck -- reviewed\n  !!str\n  |\n  . ./scripts/check.sh", "", "", false},
			{"split-anchor", "run:\n  !!str\n  &script # actionlint:ignore shellcheck -- reviewed\n  |\n  . ./scripts/check.sh\nname: *script", "", "", false},
			{"missing-reason", "run: . ./scripts/check.sh # actionlint:ignore shellcheck", "", "inline-suppression", true},
			{"forbidden", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions", true},
			{"report-suppression", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: {report: suppression}}\n", "disallow-suppressions", false},
			{"report-violation", "run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: {report: violation}}\n", "", true},
			{"other-declaration", "run: . ./scripts/check.sh\nenv: {OTHER: value} # actionlint:ignore shellcheck -- reviewed", "", "", true},
		} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("composite=%t/%s/%q", composite, tc.name, ending), func(t *testing.T) {
					root, _ := executableFixture(t)
					writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n"+tc.config)
					script := writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
					script, err := filepath.EvalSymlinks(script)
					if err != nil {
						t.Fatal(err)
					}
					declaration := strings.ReplaceAll(tc.declaration, "\n", ending)
					steps := "- shell: bash\n  working-directory: .\n  " + strings.ReplaceAll(declaration, "\n", "\n  ")
					origin := ".github/workflows/composite.yml"
					if composite {
						origin = "local/action.yml"
						writeShellcheckFixture(t, root, origin, "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
						steps = "- uses: ./local"
					}
					result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
					counts := map[string]int{}
					for _, diagnostic := range result.Diagnostics {
						counts[diagnostic.Rule]++
						if diagnostic.Rule == "shellcheck" {
							if filepath.ToSlash(diagnostic.Path) != "scripts/check.sh" || diagnostic.Code != "SC2086" || diagnostic.Severity != "info" || diagnostic.Start != (DiagnosticPosition{1, 6}) || diagnostic.End != (DiagnosticPosition{1, 12}) || diagnostic.Snippet != "echo $VALUE" {
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

func TestForeignSuppressionPreservesDiagnosticPath(t *testing.T) {
	source := []byte("name: test # actionlint:ignore action\n")
	finding := &Error{Filepath: "./local/action.yml", Line: 2, Kind: "expression", source: source}
	got := filterForeignInlineSuppressions("/repo/local/action.yml", source, []*Error{finding}, nil)
	if len(got) != 2 || got[0].Filepath != finding.Filepath || got[1].Filepath != "/repo/local/action.yml" || got[1].Kind != "inline-suppression" {
		t.Fatalf("foreign filtering changed diagnostic identity: %+v", got)
	}
	if got[0] == finding || string(got[0].source) != string(source) || finding.Filepath != "./local/action.yml" {
		t.Fatalf("foreign filtering mutated the original finding: %+v", finding)
	}
}

func TestSourcedShellcheckOriginIsolation(t *testing.T) {
	command := shellcheckForTest(t)
	const directive = " # actionlint:ignore shellcheck -- reviewed"
	for _, composite := range []bool{false, true} {
		for _, both := range []bool{false, true} {
			t.Run(fmt.Sprintf("composite=%t/both=%t", composite, both), func(t *testing.T) {
				root, _ := executableFixture(t)
				writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
				writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				step := "- shell: bash\n  working-directory: .\n  run: . ./scripts/check.sh"
				steps := step + directive + "\n" + step
				if both {
					steps += directive
				}
				if composite {
					writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
					writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./local\n")
					steps = "- uses: ./outer"
				}
				result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
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
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
	steps := "- shell: bash\n  env: {SCRIPT: &script '. ./scripts/check.sh'}\n  run: echo safe\n- shell: bash\n  run: echo safe # actionlint:ignore shellcheck -- other run\n- shell: bash\n  working-directory: .\n  run: *script"
	result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("alias origin crossed intervening declarations: %+v", result.Diagnostics)
	}
}

func TestSourcedShellcheckMetadataScope(t *testing.T) {
	command := shellcheckForTest(t)
	for _, ignoredPath := range []string{"", "scripts/**", "local/**"} {
		t.Run(ignoredPath, func(t *testing.T) {
			root, _ := executableFixture(t)
			config := "tools: {shellcheck: true}\npolicy: {disallow-suppressions: true}\nlint: {rules: {policy: {require-job-timeout: {level: on, options: {min-minutes: 5}}}}}\noverrides:\n  - includes: ['local/**']\n    lint: {rules: {policy: {require-job-timeout: {level: on, options: {max-minutes: 3}}, disallow-suppressions: off}}}\n  - includes: ['alias/**']\n    lint: {rules: {policy: {disallow-suppressions: warn}}}\n"
			if ignoredPath != "" {
				config += "paths:\n  '" + ignoredPath + "':\n    ignore: ['SC2086']\n"
			}
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
			writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
			writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: .\n      run: . ./scripts/check.sh # actionlint:ignore shellcheck -- reviewed\n")
			if err := os.Symlink(filepath.Join(root, "local"), filepath.Join(root, "alias")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			result := compositeAnalysis(t, root, "- uses: ./local\n- uses: ./alias", AnalysisOptions{Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
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
	command := shellcheckForTest(t)
	for _, suffix := range []string{".sh", ".yml", ".yaml"} {
		for _, composite := range []bool{false, true} {
			for _, tc := range []struct{ name, directive, policy string }{
				{"valid", "# actionlint:ignore shellcheck -- reviewed", ""},
				{"malformed", "# actionlint:ignore shellcheck", ""},
				{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n"},
			} {
				t.Run(fmt.Sprintf("%s/composite=%t/%s", suffix, composite, tc.name), func(t *testing.T) {
					root, _ := executableFixture(t)
					writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n"+tc.policy)
					name := "scripts/check" + suffix
					writeShellcheckFixture(t, root, name, "echo $VALUE "+tc.directive+"\n")
					steps := "- run: . ./" + name + "\n  shell: bash\n  working-directory: ."
					if composite {
						writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: . ./"+name+"\n")
						steps = "- uses: ./local"
					}
					result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
					if len(result.Diagnostics) != 1 {
						t.Fatalf("sourced script comment changed diagnostics: %+v", result.Diagnostics)
					}
					finding := result.Diagnostics[0]
					if finding.Rule != "shellcheck" || finding.Code != "SC2086" || filepath.ToSlash(finding.Path) != name || finding.Start != (DiagnosticPosition{Line: 1, Column: 6}) {
						t.Fatalf("sourced finding lost or replaced: %+v", finding)
					}
				})
			}
		}
	}
}

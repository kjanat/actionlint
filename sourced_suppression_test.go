package actionlint

import (
	"fmt"
	"path/filepath"
	"testing"
)

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

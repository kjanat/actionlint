package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestExpressionConversionsThroughCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, expression, rule, token string }{
		{"object-json", "fromJSON(steps.check.outputs).available", "expression-call", "steps.check.outputs"},
		{"boolean-json", "fromJSON(true)", "", ""},
		{"number-json", "fromJSON(42)", "", ""},
		{"output-json", "fromJSON(steps.check.outputs.available)", "", ""},
		{"string-condition", "steps.check.outputs.available", "string-conditions", "steps.check.outputs.available"},
		{"true-comparison", "steps.check.outputs.available == 'true'", "", ""},
		{"false-comparison", "steps.check.outputs.available == 'false'", "", ""},
		{"boolean-true", "true", "", ""},
		{"boolean-false", "false", "", ""},
		{"quoted-list-join", "join('[github.server_url,github.repository]', '/')", "join-literal", "'[github.server_url,github.repository]'"},
		{"ordinary-join", "join('ordinary text', '/')", "join-literal", "'ordinary text'"},
		{"array-join", `join(fromJSON('["a","b"]'), '/')`, "", ""},
		{"dynamic-join", "join(inputs.value, '/')", "", ""},
		{"object-equality", `fromJSON('{}') == fromJSON('{}')`, "collection-comparison", `fromJSON('{}') == fromJSON('{}')`},
		{"array-inequality", `fromJSON('[]') != fromJSON('[]')`, "collection-comparison", `fromJSON('[]') != fromJSON('[]')`},
		{"object-null", `fromJSON('{}') != null`, "", ""},
		{"scalar-json-equality", `fromJSON('42') == fromJSON('42')`, "", ""},
	} {
		for _, kind := range []string{"workflow", "composite", "composite-output"} {
			for _, selection := range []string{"off", "selected", "suppressed"} {
				t.Run(tc.name+"/"+kind+"/"+selection, func(t *testing.T) {
					t.Parallel()
					root := t.TempDir()
					config := "tools: {shellcheck: false, ruff: false}\nlint:\n  rules:\n    preset: none\n    correctness:\n      expression-call: error\n      expression-type: error\n"
					if selection != "off" {
						config += "    suspicious:\n      string-conditions: warn\n      join-literal: info\n      collection-comparison: error\n"
					}
					configPath := filepath.Join(root, "actionlint.yaml")
					if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					var source, name string
					if kind != "workflow" {
						name = "action.yml"
						source = "name: expression conversion\ndescription: Check independent expression cases.\ninputs:\n  value:\n    description: Dynamic text.\nruns:\n  using: composite\n  steps:\n    - id: check\n      shell: bash\n      run: echo available=false >> \"$GITHUB_OUTPUT\"\n    - shell: bash\n      run: echo checked\n      if: ${{ " + tc.expression + " }}\n"
						if kind == "composite-output" {
							source = strings.Replace(source, "      if: ${{ "+tc.expression+" }}\n", "", 1)
							source += "outputs:\n  result:\n    description: Evaluated output.\n    value: ${{ " + tc.expression + " }}\n"
						}
					} else {
						name = "workflow.yml"
						source = "on:\n  workflow_dispatch:\n    inputs:\n      value:\n        type: string\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - id: check\n        run: echo available=false >> \"$GITHUB_OUTPUT\"\n      - run: echo checked\n        if: ${{ " + tc.expression + " }}\n"
					}
					if selection == "suppressed" && tc.rule != "" {
						source = strings.Replace(source, " }}\n", " }} # ignore["+tc.rule+"]\n", 1)
					}
					path := filepath.Join(root, name)
					if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
						t.Fatal(err)
					}
					var out, log bytes.Buffer
					command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
					status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", configPath, "--output-format=json", "--summary=false", path})
					if status > 1 {
						t.Fatalf("status=%d stderr=%s output=%s", status, log.String(), out.String())
					}
					var report resultreport.CheckResult
					if err := json.Unmarshal(out.Bytes(), &report); err != nil {
						t.Fatalf("decode: %v: %s", err, out.String())
					}
					want := tc.rule
					if kind == "composite-output" && want == "string-conditions" {
						want = ""
					}
					if selection == "off" && want != "expression-call" || selection == "suppressed" {
						want = ""
					}
					if want == "" {
						if len(report.Diagnostics) != 0 {
							t.Fatalf("unexpected diagnostics: %+v", report.Diagnostics)
						}
						return
					}
					if len(report.Diagnostics) != 1 {
						t.Fatalf("want %s, diagnostics=%+v", want, report.Diagnostics)
					}
					finding := report.Diagnostics[0]
					if finding.Rule != want || finding.Start.Line != finding.End.Line {
						t.Fatalf("unexpected finding: %+v", finding)
					}
					line := []rune(strings.Split(source, "\n")[finding.Start.Line-1])
					if got := string(line[finding.Start.Column-1 : finding.End.Column-1]); got != tc.token {
						t.Fatalf("range=%q want %q: %+v", got, tc.token, finding)
					}
				})
			}
		}
	}
}

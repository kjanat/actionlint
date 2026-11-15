package checking

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestAdditionalLanguageLifecycleUsesExistingPipeline(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeShellcheckFixture(t, root, "helper/action.yml", "name: helper\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: python example.py\n      shell: bash\n")
	config, err := ConfigurationRules().Parse([]byte("lint:\n  rules: {correctness: {runtime-lifecycle: warn}}\n  lifecycle:\n    policies:\n      - id: plan-language-upgrades\n        description: Plan language upgrades before support ends\n        match: {subjects: [language-runtimes], events: [support-ends]}\n        notifications: [{starts: {before: 180d}, severity: error}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	asOf, err := lifecycle.ParseDate("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := lifecycle.ParseDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	github, err := services.Parse("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	evidence := lifecycle.Evidence{Subject: lifecycle.LanguageRuntimes, Name: "Python 3.12", Kind: lifecycle.SupportEnds, AsOf: asOf, Effective: effective, Service: github, Source: "https://example.com/python-support"}
	workflow := "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/helper\n"
	visitedScripts := 0
	analysis, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources:    []sourceUnit{{Path: "ci.yml", Project: workspace.Identity(root), Config: config, Content: []byte(workflow)}},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			check := checkinputs.DeclarationCheck{
				Declaration: correctness.RuntimeLifecycleDeclaration(),
				Step: func(step workflowdecl.Step) []reporting.StepFinding {
					if _, run := step.Run(); !run {
						return nil
					}
					visitedScripts++
					position, present := step.Position()
					return []reporting.StepFinding{{Rule: "runtime-lifecycle", Code: string(evidence.Kind), Message: "Plan a Python upgrade", Start: reporting.Position(reporting.OptionalPosition(position, present)), Step: step, LifecycleEvidence: &evidence}}
				},
			}
			return []workflowvisit.Rule{declarationchecks.New(ConfigurationRules(), check)}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Diagnostics) != 1 {
		t.Fatalf("new producer lost its composite finding: scripts=%d diagnostics=%+v documents=%+v incomplete=%+v", visitedScripts, analysis.Diagnostics, analysis.Documents, analysis.IncompleteChecks)
	}
	diagnostic := analysis.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Lifecycle == nil || diagnostic.Lifecycle.Name != evidence.Name || len(diagnostic.Lifecycle.Policies) != 1 || diagnostic.Lifecycle.Policies[0].ID != "plan-language-upgrades" {
		t.Fatalf("new producer did not receive the prepared policy: %+v", diagnostic)
	}
	var encoded bytes.Buffer
	if err := output.WriteJSON(&encoded, report.FromAnalysis(analysis.Analysis), false); err != nil {
		t.Fatal(err)
	}
	var jsonReport report.CheckResult
	if err := json.Unmarshal(encoded.Bytes(), &jsonReport); err != nil {
		t.Fatal(err)
	}
	if len(jsonReport.Diagnostics) != 1 || !reflect.DeepEqual(jsonReport.Diagnostics[0].Lifecycle, diagnostic.Lifecycle) {
		t.Fatalf("JSON lost the new producer's lifecycle report: %s", encoded.Bytes())
	}
	encoded.Reset()
	if err := output.WriteSARIF(&encoded, "test", nil, analysis.Diagnostics); err != nil {
		t.Fatal(err)
	}
	var sarif struct {
		Runs []struct {
			Results []struct {
				Properties struct {
					Lifecycle *results.LifecycleReport `json:"lifecycle"`
				} `json:"properties"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &sarif); err != nil {
		t.Fatal(err)
	}
	if len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != 1 || !reflect.DeepEqual(sarif.Runs[0].Results[0].Properties.Lifecycle, diagnostic.Lifecycle) {
		t.Fatalf("SARIF lost the new producer's lifecycle report: %s", encoded.Bytes())
	}
}

package output

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestPrettyCompactsDependenciesAndExplicitRequestShowsDetails(t *testing.T) {
	t.Parallel()
	renderer, err := New(outputformat.Pretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := publishedFinding(results.Diagnostic{Origin: results.DependencyOrigin, Path: "https://github.com/example/action/blob/abc/action.yml", Rule: "shellcheck", Severity: severity.Info, Message: "dependency detail", Related: []results.Related{{Message: results.IdenticalDependencyLocation, Path: "https://github.com/example/action/blob/def/action.yml"}}}, []byte("name: action"))
	local := publishedFinding(results.Diagnostic{Origin: results.ProjectOrigin, Path: "ci.yml", Rule: "action", Message: "project detail"}, nil)
	report := RenderedReport{Findings: []results.PublishedDiagnostic{d, local}}
	var compact, verbose strings.Builder
	if err := renderer.Render(&compact, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dependency: example/action@abc, def: 0 errors, 0 warnings, 1 info", "project detail", "actionlint check --dependency-findings=full"} {
		if !strings.Contains(compact.String(), want) {
			t.Fatalf("compact output lacks %q:\n%s", want, compact.String())
		}
	}
	if strings.Contains(compact.String(), "dependency detail") {
		t.Fatalf("compact output contains full dependency details:\n%s", compact.String())
	}
	renderer.SetShowDependencies(true)
	if err := renderer.Render(&verbose, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verbose.String(), "info[dependency:shellcheck]") || !strings.Contains(verbose.String(), "dependency detail") || strings.Contains(verbose.String(), "actionlint check --dependency-findings=full") {
		t.Fatalf("verbose output lacks full dependency details:\n%s", verbose.String())
	}
	if err := printDependencySummary(failingRenderWriter{}, report.Findings); err == nil {
		t.Fatal("lost compact dependency write error")
	}
}

func TestStructuredDependencyOriginKeepsRuleIdentity(t *testing.T) {
	t.Parallel()
	diagnostic := results.Diagnostic{Rule: "shellcheck", Code: "SC2086", Origin: results.DependencyOrigin, Message: "Quote the variable", Severity: severity.Info}
	var out strings.Builder
	if err := WriteSARIF(&out, testVersion(), nil, []results.Diagnostic{diagnostic}); err != nil {
		t.Fatal(err)
	}
	var document sarifDocument
	if err := json.Unmarshal([]byte(out.String()), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
		t.Fatalf("missing SARIF result: %s", out.String())
	}
	got := document.Runs[0].Results[0]
	if got.RuleID != "shellcheck" || got.Properties == nil || got.Properties.Origin != results.DependencyOrigin || got.Properties.DiagnosticCode != "SC2086" {
		t.Fatalf("SARIF changed the rule identity or lost dependency origin: %+v", got)
	}
	data, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	var decoded results.Diagnostic
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Rule != "shellcheck" || decoded.Origin != results.DependencyOrigin {
		t.Fatalf("JSON changed the rule identity or lost dependency origin: %s", data)
	}
}

func TestPrettyAlwaysShowsFailingDependencyFindings(t *testing.T) {
	t.Parallel()
	for _, threshold := range []severity.Severity{severity.Error, severity.Warning} {
		renderer, err := New(outputformat.Pretty, "", false, testVersion, nil)
		if err != nil {
			t.Fatal(err)
		}
		var findings []results.PublishedDiagnostic
		for _, level := range []severity.Severity{severity.Error, severity.Warning, severity.Info} {
			findings = append(findings, publishedFinding(results.Diagnostic{Origin: results.DependencyOrigin, Path: "https://github.com/example/action/blob/abc/action.yml", Rule: "shellcheck", Severity: level, Message: string(level) + " dependency detail"}, []byte("name: action")))
		}
		var out strings.Builder
		if err := renderer.Render(&out, RenderedReport{FailureThreshold: threshold, Findings: findings}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "error dependency detail") || strings.Contains(out.String(), "info dependency detail") || strings.Contains(out.String(), "warning dependency detail") != (threshold == severity.Warning) {
			t.Fatalf("threshold=%s: failing dependency details hidden or nonfailing details expanded:\n%s", threshold, out.String())
		}
	}
}

func TestCompactDependenciesKeepDifferentContentsAndUnknownSourcesSeparate(t *testing.T) {
	t.Parallel()
	for _, unknown := range []bool{false, true} {
		var findings []results.PublishedDiagnostic
		for i, pin := range []string{"abc", "def"} {
			content := []byte("name: " + pin)
			if unknown {
				content = nil
			}
			d := publishedFinding(results.Diagnostic{Origin: results.DependencyOrigin, Path: "https://github.com/example/action/blob/" + pin + "/action.yml", Rule: "action", Severity: []severity.Severity{severity.Error, severity.Warning}[i]}, content)
			findings = append(findings, d)
		}
		var out strings.Builder
		if err := printDependencySummary(&out, findings); err != nil {
			t.Fatal(err)
		}
		if strings.Count(out.String(), "dependency: example/action@") != 2 {
			t.Fatalf("unknown=%v: different source revisions merged:\n%s", unknown, out.String())
		}
	}
}

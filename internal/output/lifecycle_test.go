package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sarifcheck "actionlint.kjanat.dev/v2/internal/development/tests/support/sarif"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestLifecycleOutput(t *testing.T) {
	t.Parallel()
	lifecycle := &results.LifecycleReport{
		Subject: "runner", Name: "ubuntu-22.04", Event: "retirement", Source: "https://example.com/notice",
		AnnouncedOn: "2026-01-01", EffectiveOn: "2026-12-01", AsOf: "2026-11-15",
		Policies: []results.LifecyclePolicyReport{
			{ID: "retirement-warning", Description: "Warn before retirement", Origin: "project", Severity: "warning", StartsOn: "2026-09-01"},
			{ID: "retirement-required", Description: "Require migration", Origin: "organization", Severity: "error", StartsOn: "2026-11-01", Enforced: true},
		},
	}
	diagnostic := results.Diagnostic{Rule: "runner", Code: "retiring", Message: "Runner is retiring", Lifecycle: lifecycle}
	want := map[string]any{
		"subject": "runner", "name": "ubuntu-22.04", "event": "retirement", "source": "https://example.com/notice",
		"announced_on": "2026-01-01", "effective_on": "2026-12-01", "as_of": "2026-11-15",
		"policies": []any{
			map[string]any{"id": "retirement-warning", "description": "Warn before retirement", "origin": "project", "severity": "warning", "starts_on": "2026-09-01", "enforced": false},
			map[string]any{"id": "retirement-required", "description": "Require migration", "origin": "organization", "severity": "error", "starts_on": "2026-11-01", "enforced": true},
		},
	}
	for _, format := range []Format{FormatJSON, FormatJSONL, FormatSARIF} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			renderer, err := New(format, "", false, testVersion, nil)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := renderer.Render(&out, Result{Status: "success", Diagnostics: []results.Diagnostic{diagnostic, {Rule: "other", Message: "ordinary"}}}); err != nil {
				t.Fatal(err)
			}
			var findings []map[string]any
			switch format {
			case FormatJSON:
				var document struct {
					Diagnostics []map[string]any `json:"diagnostics"`
				}
				if err := json.Unmarshal(out.Bytes(), &document); err != nil {
					t.Fatal(err)
				}
				findings = document.Diagnostics
			case FormatJSONL:
				decoder := json.NewDecoder(&out)
				for range 2 {
					var finding map[string]any
					if err := decoder.Decode(&finding); err != nil {
						t.Fatal(err)
					}
					findings = append(findings, finding)
				}
			case FormatSARIF:
				sarifcheck.RequireValid(t, out.Bytes())
				var document struct {
					Runs []struct {
						Results []map[string]any `json:"results"`
					} `json:"runs"`
				}
				if err := json.Unmarshal(out.Bytes(), &document); err != nil {
					t.Fatal(err)
				}
				for _, result := range document.Runs[0].Results {
					if properties, ok := result["properties"].(map[string]any); ok {
						findings = append(findings, properties)
					} else {
						findings = append(findings, nil)
					}
				}
			default:
				t.Fatalf("unexpected format %q", format)
			}
			if !reflect.DeepEqual(findings[0]["lifecycle"], want) {
				t.Fatalf("lifecycle metadata = %#v, want %#v", findings[0]["lifecycle"], want)
			}
			if _, ok := findings[1]["lifecycle"]; ok {
				t.Fatalf("ordinary finding gained lifecycle metadata: %#v", findings[1])
			}
		})
	}
	t.Run("pretty", func(t *testing.T) {
		t.Parallel()
		renderer, err := New(FormatPretty, "", false, testVersion, []Rule{{Name: "runner", Category: "workflow"}})
		if err != nil {
			t.Fatal(err)
		}
		renderer.SetHyperlinks(true)
		var out bytes.Buffer
		if err := renderer.Render(&out, Result{Findings: []Finding{FindingOf(diagnostic, nil)}}); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"config: lint.rules.workflow.runner", "lifecycle: retirement on 2026-12-01", "https://example.com/notice", "policy: retirement-warning (optional), starts 2026-09-01, from project", "policy: retirement-required (required), starts 2026-11-01, from organization", "\x1b]8;;https://example.com/notice\x1b\\"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("missing %q in %q", want, out.String())
			}
		}
	})
}

func TestLifecycleOptionalDatesAreOmitted(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(results.Diagnostic{Lifecycle: &results.LifecycleReport{}})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic map[string]any
	if err := json.Unmarshal(data, &diagnostic); err != nil {
		t.Fatal(err)
	}
	lifecycle, ok := diagnostic["lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("missing lifecycle report: %s", data)
	}
	for _, field := range []string{"announced_on", "effective_on"} {
		if _, ok := lifecycle[field]; ok {
			t.Errorf("empty %s was serialized: %s", field, data)
		}
	}
}

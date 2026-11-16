package action_test

import (
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestRuntimeLifecycleEvidencePreservesValidationLocations(t *testing.T) {
	t.Parallel()
	const source = "name: example\ndescription: example\nruns:\n  using: node20\n  main: index.js\n"
	meta, err := actionmeta.ParseActionMetadata("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	date, err := lifecycle.ParseDate("2026-09-22")
	if err != nil {
		t.Fatal(err)
	}
	service, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	context := actionmeta.LifecycleContext{AsOf: date, Service: service}
	caller := &sourcepos.Position{Line: 8, Column: 11}
	exec := ownedActionFixture(&definition.ExecAction{Uses: &definition.String{Value: "example/script@v1", Pos: caller}})
	for _, tc := range []struct {
		name     string
		findings []*findingmodel.Finding
		line     int
	}{
		{"standalone", actioncheck.StandaloneWithAdvisories(meta, actionmeta.ReferencedFiles{}, context), 4},
		{"local call", actioncheck.CheckWithAdvisories(meta, caller, actionmeta.ReferencedFiles{}, context), 8},
		{"repository call", actioncheck.Repository(exec, actionmeta.NewDefinition(meta), context), 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if len(tc.findings) != 1 {
				t.Fatalf("findings: %+v", tc.findings)
			}
			f := tc.findings[0]
			if f.Start.Line != tc.line || f.LifecycleEvidence == nil || f.LifecycleEvidence.Current || f.LifecycleEvidence.AsOf != date || f.LifecycleEvidence.Service != service || f.Code != string(actionmeta.CodeRuntimeDeprecated) {
				t.Fatalf("lost future evidence or source location: %+v", f)
			}
		})
	}
}

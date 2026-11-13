package scripts

import (
	"encoding/json"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
	pythonprofile "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/python"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestSourcePreparationMapsGeneratedRowsForEveryProfile(t *testing.T) {
	t.Parallel()
	settings, err := config.ToolConfigInline("example", map[string]any{"limit": 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{mask.ID, pythonprofile.ID} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			manifest := manifests.Manifest{
				Rule: rule.Descriptor{Name: "example"}, Preparation: profile,
				ConfigSchema:         json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`),
				PreparationTransform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: `{source_prefix:"# generated\n",startup:"first\nsecond",config:{threshold:.settings.options.limit},variables:{mode:.language},excluded:["X1"]}`},
			}
			script := tools.Script{Text: "print(1)\n", Shell: new("python"), Config: &config.Config{Tools: config.ToolsConfig{"example": {Config: settings}}}}
			prepared, err := newPreparation(manifest, routeEngine, nil).input(t.Context(), script)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Source != "# generated\nfirst\nsecond\nprint(1)\n" {
				t.Fatalf("source=%q", prepared.Source)
			}
			if string(prepared.Config) != `{"threshold":7}` || prepared.Vars["mode"] != "python" || len(prepared.Excluded) != 1 || prepared.Excluded[0] != "X1" {
				t.Fatalf("prepared=%+v", prepared)
			}
			for row, origin := range map[int]preparation.LineOrigin{1: preparation.LineDirective, 2: preparation.LineStartup, 3: preparation.LineStartup, 4: preparation.LineScript} {
				line := prepared.LineMap(row)
				if line.Origin != origin || row == 4 && line.Line != 1 {
					t.Fatalf("row %d=%+v", row, line)
				}
			}
		})
	}
}

func TestSourcePreparationComposesPythonMaskFilter(t *testing.T) {
	t.Parallel()
	script := tools.Script{Text: "__all__ = ['${{ inputs.name }}']\nprint(real_name)\n"}
	prepared, err := pythonprofile.Script(script.Text)
	if err != nil {
		t.Fatal(err)
	}
	transform, err := preparation.NewSourceTransform(&manifests.Transform{Language: manifests.LanguageJQ, Inline: `{source_prefix:"# generated\n",startup:"first\nsecond"}`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = applySourcePreparation(t.Context(), transform, script, prepared, integrationconfig.Settings{}, routeEngine)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(prepared.Source, "\n")
	column := 1
	finding := checkinputs.ScriptDiagnostic{Location: checkinputs.ScriptPosition{Row: 4, Column: column}, EndLocation: checkinputs.ScriptPosition{Row: 4, Column: len(lines[3]) + 1}, TemplateEffect: checkinputs.TemplateEffectExportedName}
	if prepared.AcceptDiagnostic(finding) {
		t.Fatal("generated Python identifier was reported after row insertion")
	}
	finding.Location.Row = 5
	finding.EndLocation.Row = 5
	if !prepared.AcceptDiagnostic(finding) {
		t.Fatal("real Python identifier was discarded")
	}
	if line := prepared.LineMap(5); line.Line != 2 || line.Origin != preparation.LineScript {
		t.Fatalf("line=%+v", line)
	}
}

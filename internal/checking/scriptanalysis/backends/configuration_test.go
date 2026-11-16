package backends

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/manifests"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/preparation/mask"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/preparation/python"
)

func TestConfigurationTransformsReachEachOptionsOnlyProfile(t *testing.T) {
	t.Parallel()
	settings, err := configuration.ToolConfigInline("example", map[string]any{"limit": 7})
	if err != nil {
		t.Fatal(err)
	}
	script := scriptanalysis.Unit{Run: runBody("pass\n", nil), Scope: &scriptanalysis.Scope{Config: &configuration.Config{Tools: configuration.ToolsConfig{"example": {Config: settings}}}}}
	for _, profile := range []string{mask.ID, python.ID} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			manifest := manifests.Manifest{
				Rule: rule.Descriptor{Name: "example"}, Preparation: profile,
				ConfigSchema:           json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"limit":{"type":"integer"}}}`),
				ConfigurationTransform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: `{options:{threshold:.config.limit}}`},
			}
			prepared, err := newPreparation(manifest, routeEngine, nil).input(t.Context(), script)
			if err != nil {
				t.Fatal(err)
			}
			if string(prepared.Config) != `{"threshold":7}` {
				t.Fatalf("converted options were lost: %s", prepared.Config)
			}
		})
	}
}

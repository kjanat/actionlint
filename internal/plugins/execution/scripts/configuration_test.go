package scripts

import (
	"encoding/json"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
	pythonprofile "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/python"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestConfigurationTransformsReachEachOptionsOnlyProfile(t *testing.T) {
	t.Parallel()
	settings, err := config.ToolConfigInline("example", map[string]any{"limit": 7})
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Text: "pass\n", Config: &config.Config{Tools: config.ToolsConfig{"example": {Config: settings}}}}
	for _, profile := range []string{mask.ID, pythonprofile.ID} {
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

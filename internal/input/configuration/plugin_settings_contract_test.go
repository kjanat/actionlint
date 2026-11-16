package configuration_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestRegisteredNurseryPresetSelection(t *testing.T) {
	t.Parallel()
	descriptor := rule.Declaration{Name: "experimental-plugin", Maturity: "nursery"}
	for _, test := range []struct {
		root, group configuration.Preset
		want        configuration.Level
	}{
		{"", "", "off"}, {"all", "", "off"}, {"", "all", "off"}, {"recommended", "", "off"}, {"none", "", "off"},
	} {
		cfg := &configuration.Config{}
		cfg.Lint.Rules.Preset = test.root
		cfg.Lint.Rules.Plugins = configuration.NewGroup(test.group, "", nil)
		if got := checking.ConfigurationRules().SettingFor(cfg, descriptor, configuration.Presets{}, true).Level(); got != test.want {
			t.Errorf("root=%s group=%s: %s want %s", test.root, test.group, got, test.want)
		}
	}
}

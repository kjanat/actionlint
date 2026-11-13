package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestRegisteredNurseryPresetSelection(t *testing.T) {
	t.Parallel()
	descriptor := repoconfig.Rule{Name: "experimental-plugin", Maturity: "nursery", Group: "plugins"}
	for _, test := range []struct {
		root, group repoconfig.Preset
		want        repoconfig.Level
	}{
		{"", "", "off"}, {"all", "", "off"}, {"", "all", "off"}, {"recommended", "", "off"}, {"none", "", "off"},
	} {
		cfg := &repoconfig.Config{}
		cfg.Lint.Rules.Preset = test.root
		cfg.Lint.Rules.Plugins = repoconfig.NewGroup(test.group, "", nil)
		if got := checking.ConfigurationRules().SettingFor(cfg, descriptor, repoconfig.Presets{}, true).Level(); got != test.want {
			t.Errorf("root=%s group=%s: %s want %s", test.root, test.group, got, test.want)
		}
	}
}

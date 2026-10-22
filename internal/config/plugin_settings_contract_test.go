package config_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestRegisteredNurseryPresetSelection(t *testing.T) {
	descriptor := repoconfig.Rule{Name: "experimental-plugin", Maturity: "nursery", Group: "plugins"}
	for _, test := range []struct {
		root, group repoconfig.Preset
		want        repoconfig.Level
	}{
		{"", "", "on"}, {"all", "", "off"}, {"", "all", "off"}, {"recommended", "", "off"}, {"none", "", "off"},
	} {
		cfg := &repoconfig.Config{}
		cfg.Lint.Rules.Preset = test.root
		cfg.Lint.Rules.Plugins.Preset = test.group
		if got := repoconfig.SettingFor(cfg, descriptor, repoconfig.Presets{}, true).Level; got != test.want {
			t.Errorf("root=%s group=%s: %s want %s", test.root, test.group, got, test.want)
		}
	}
}

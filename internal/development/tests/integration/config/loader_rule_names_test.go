package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestAdditionalRulesAreScopedToResolution(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"lint: {rules: {plugins: {no-sudo: {level: warn, options: {key: value}}}}}",
		"lint: {rules: {disable: [no-sudo]}}",
		"ignores: [{rule: no-sudo, file: '**'}]",
		"policy: {disallow-suppressions: {rules: [no-sudo]}}",
		"lint: {rules: {policy: {disallow-suppressions: {level: error, options: {rules: [no-sudo]}}}}}",
	} {
		if _, err := checking.ConfigurationRules().Parse([]byte(source), "no-sudo"); err != nil {
			t.Fatal(err)
		}
		cfg, err := checking.ConfigurationRules().Parse([]byte(source))
		if err == nil {
			_, err = checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, cfg)
		}
		if err == nil {
			t.Fatalf("additional rule leaked: %s", source)
		}
		overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte(source), "no-sudo")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := checking.ConfigurationRules().Overlaid(nil, nil, []repoconfig.Overlay{overlay}, "no-sudo"); err != nil {
			t.Fatal(err)
		}
		cfg, _, err = checking.ConfigurationRules().Overlaid(nil, nil, []repoconfig.Overlay{overlay})
		if err == nil {
			_, err = checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, cfg)
		}
		if err == nil {
			t.Fatal("overlay skipped rule validation")
		}
	}
}

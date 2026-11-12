package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestNativeRuffConfigRoundTrip(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte(`tools:
  ruff:
    config:
      target-version: py312
      lint: {extend-select: [B, S], ignore: [F401]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if settings := cfg.Tools["ruff"].Config.Inline(); settings["target-version"] != "py312" {
		t.Fatal(settings)
	}
}

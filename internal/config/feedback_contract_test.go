package config_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestNativeRuffConfigRoundTrip(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`tools:
  ruff:
    config:
      target-version: py312
      lint: {extend-select: [B, S], ignore: [F401]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Ruff.Native["target-version"] != "py312" {
		t.Fatal(cfg.Tools.Ruff.Native)
	}
}

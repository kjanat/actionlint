package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references"
)

func TestMetadataMirrorConfiguration(t *testing.T) {
	rules := checking.ConfigurationRules()
	for _, tc := range []struct {
		name, input string
		want        references.MirrorAccess
	}{
		{"default", `{}`, references.PublicMirrorsOnly},
		{"disabled", `{"services":[]}`, references.NoMirrors},
		{"unknown allowed", `{"allow-unknown-repositories":true}`, references.AllowUnknownMirrors},
		{"no services wins", `{"services":[],"allow-unknown-repositories":true}`, references.NoMirrors},
		{"explicit default", `{"services":["jsdelivr"],"allow-unknown-repositories":false}`, references.PublicMirrorsOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := rules.Parse([]byte(`{"network":{"metadata-mirrors":` + tc.input + `}}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.MetadataMirrorAccess(); got != tc.want {
				t.Fatalf("mirror access = %v, want %v", got, tc.want)
			}
			encoded, err := rules.EncodeValue(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var decoded config.Config
			if err := rules.DecodeValue(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if got := decoded.MetadataMirrorAccess(); got != tc.want {
				t.Fatalf("mirror access after round trip = %v, want %v; encoded = %v", got, tc.want, encoded)
			}
		})
	}
	for _, invalid := range []string{`{"services":["typo"]}`, `{"services":"jsdelivr"}`, `{"allow-unknown-repositories":"yes"}`, `{"allow-unknown-repository":true}`} {
		if _, err := rules.Parse([]byte(`{"network":{"metadata-mirrors":` + invalid + `}}`)); err == nil {
			t.Errorf("accepted invalid configuration %s", invalid)
		}
	}
}

func TestMetadataMirrorConfigurationExtends(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base.toml")
	if err := os.WriteFile(base, []byte("[network.metadata-mirrors]\nservices = ['jsdelivr']\nallow-unknown-repositories = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, override string
		want           references.MirrorAccess
	}{
		{"inherited", "", references.AllowUnknownMirrors},
		{"disable", "[network.metadata-mirrors]\nservices = []\n", references.NoMirrors},
		{"public only", "[network.metadata-mirrors]\nallow-unknown-repositories = false\n", references.PublicMirrorsOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.name+".toml")
			if err := os.WriteFile(path, []byte("extends = ['base.toml']\n"+tc.override), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := checking.ConfigurationRules().Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.MetadataMirrorAccess(); got != tc.want {
				t.Fatalf("mirror access = %v, want %v", got, tc.want)
			}
		})
	}
}

package lintconfig

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestSettingCanonicalLevels(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"false", "off"},
		{"{level: false}", "off"},
		{"null", "default"},
		{"{level: null}", "default"},
		{"off", "off"},
		{"default", "default"},
		{"{level: default}", "default"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var setting Setting
			if err := yaml.Unmarshal([]byte(tc.input), &setting); err != nil {
				t.Fatal(err)
			}
			encoded, err := yaml.Marshal(setting)
			if err != nil {
				t.Fatal(err)
			}
			var normalized struct {
				Level string `yaml:"level"`
			}
			if err := yaml.Unmarshal(encoded, &normalized); err != nil {
				t.Fatal(err)
			}
			if normalized.Level != tc.want {
				t.Fatalf("got %s, want %s", normalized.Level, tc.want)
			}
		})
	}
}

func TestLevelErrorsUseNamedLevels(t *testing.T) {
	for _, input := range []string{"''", "true", "0", "[]", "{}"} {
		var level Level
		err := yaml.Unmarshal([]byte(input), &level)
		if err == nil {
			t.Fatalf("accepted %s", input)
		}
		if strings.Contains(err.Error(), "false") || strings.Contains(err.Error(), "null") {
			t.Fatalf("compatibility aliases advertised: %s", err)
		}
	}
}

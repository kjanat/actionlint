package config

import (
	"testing"
)

func TestShellcheckConfigValidation(t *testing.T) {
	for _, value := range []string{
		`true`, `false`, `{config: './.shellcheckrc'}`,
		`{enabled: false}`, `{enabled: true, config: {}}`,
		`{config: {disable: [SC2086, SC3000-SC4000, all], enable: [all], shell: bash, extended-analysis: false, external-sources: false, source-path: ['my scripts']}}`,
		`{config: null}`, `{enabled: null, config: {shell: null, disable: null}}`,
		`{config: {disable: [&code SC2086, *code]}}`,
		`{config: {<<: {disable: [SC2086]}}}`,
	} {
		if _, err := Parse([]byte("tools: {shellcheck: " + value + "}")); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{
		`{enabled: banana}`, `{config: {format: json}}`, `{config: {severity: warning}}`,
		`{config: {disable: [2086]}}`, `{config: {disable: ['SC2086\nenable=all']}}`,
		`{config: {enable: [some_check]}}`, `{config: {shell: python}}`, `{config: {shell: ''}}`,
		`{config: {extended-analysis: yes}}`, `{config: {source-path: ['']}}`,
		`{config: {source-path: ["bad\npath"]}}`, `{config: ''}`, `{config: {typo: true}}`,
		`{config: {<<: {disable: [2086]}}}`, `{config: {disable: [&code 2086, *code]}}`,
	} {
		if _, err := Parse([]byte("tools: {shellcheck: " + value + "}")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

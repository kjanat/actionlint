package config

import (
	"testing"
)

func TestAdditionalRulesAreScopedToResolution(t *testing.T) {
	for _, source := range []string{
		"lint: {rules: {plugins: {no-sudo: {level: warn, options: {key: value}}}}}",
		"lint: {rules: {disable: [no-sudo]}}",
		"ignores: [{rule: no-sudo, file: '**'}]",
		"policy: {disallow-suppressions: {rules: [no-sudo]}}",
		"lint: {rules: {policy: {disallow-suppressions: {level: error, options: {rules: [no-sudo]}}}}}",
	} {
		if _, err := Parse([]byte(source), "no-sudo"); err != nil {
			t.Fatal(err)
		}
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("additional rule leaked: %s", source)
		}
		overlay, err := ParseOverlay("config", []byte(source), "no-sudo")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Overlaid(nil, nil, []Overlay{overlay}, "no-sudo"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Overlaid(nil, nil, []Overlay{overlay}); err == nil {
			t.Fatal("overlay skipped rule validation")
		}
	}
}

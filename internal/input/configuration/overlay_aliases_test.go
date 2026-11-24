package configuration_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// A plugin rule's alias is unknown when an overlay is parsed before plugins load.
func TestDeferredOverlayRejectsConflictingRuleAliases(t *testing.T) {
	t.Parallel()
	beforePlugins, err := configuration.NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	final, err := configuration.NewRuleSet([]rule.Declaration{{
		Name: "check-alpha", Aliases: []rule.Alias{{Name: "old-alpha"}}, Category: "correctness", Maturity: "stable",
	}})
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("lint: {rules: {correctness: {check-alpha: warn, old-alpha: error}}}")
	if _, err := final.ParseOverlay("config", source); err == nil || !strings.Contains(err.Error(), "conflicts with lint.rules.correctness.check-alpha") {
		t.Fatalf("bound overlay accepted conflicting aliases: %v", err)
	}
	overlay, err := beforePlugins.ParseOverlayBeforePlugins("config", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := final.Overlaid(nil, nil, []configuration.Overlay{overlay}); err == nil || !strings.Contains(err.Error(), "duplicate rule aliases") {
		t.Fatalf("deferred overlay dropped a conflicting alias: %v", err)
	}
}

func TestDeferredOverlayRejectsRegisteredRuleAliases(t *testing.T) {
	t.Parallel()
	beforePlugins, err := configuration.NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	final, err := beforePlugins.WithRegisteredRules(configuration.RuleRegistration{
		Declaration: rule.Declaration{
			Descriptor: rule.Descriptor{Name: "check-alpha", Aliases: []rule.Alias{{Name: "old-alpha"}}, Category: "correctness", Maturity: "stable"},
			Scope:      rule.SourceConfiguration,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"plugins", "correctness"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			source := []byte("lint: {rules: {" + category + ": {check-alpha: warn, old-alpha: error}}}")
			overlay, err := beforePlugins.ParseOverlayBeforePlugins("config", source)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := final.Overlaid(nil, nil, []configuration.Overlay{overlay}); err == nil || !strings.Contains(err.Error(), "duplicate rule aliases") {
				t.Fatalf("registered rule lost conflicting settings in %s: %v", category, err)
			}
		})
	}
}

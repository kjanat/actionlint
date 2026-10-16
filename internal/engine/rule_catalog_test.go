package engine

import (
	"testing"

	"actionlint.kjanat.dev/internal/repoconfig"
)

func TestRuleBehavioursMatchCatalog(t *testing.T) {
	names := map[string]bool{}
	for _, rule := range repoconfig.Rules() {
		names[rule.Name] = true
	}
	for name := range ruleBehaviours() {
		if !names[name] {
			t.Errorf("behaviour %q has no catalog entry", name)
		}
	}
	for _, d := range builtinRuleDescriptors() {
		if d.build == nil && d.buildScripts == nil && d.enabled == nil && d.required == nil {
			continue
		}
		if _, ok := ruleBehaviours()[d.Name]; !ok {
			t.Errorf("descriptor %q carries behaviour without a behaviour entry", d.Name)
		}
	}
}

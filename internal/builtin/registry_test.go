package builtin

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
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
	for _, d := range Descriptors() {
		if d.Build == nil && d.Script == nil && d.Enabled == nil {
			continue
		}
		if _, ok := ruleBehaviours()[d.Name]; !ok {
			t.Errorf("descriptor %q carries behaviour without a behaviour entry", d.Name)
		}
	}
}

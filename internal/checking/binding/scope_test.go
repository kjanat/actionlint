package binding_test

import (
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestPreparedDecisionsKeepScopeAndRegistryIdentity(t *testing.T) {
	t.Parallel()
	rules, err := configuration.NewRuleSet([]rule.Declaration{
		{Name: "caller", Category: "correctness", Scope: rule.CallerConfiguration},
		{Name: "source", Category: "correctness", Scope: rule.SourceConfiguration},
		{Name: "composite", Category: "correctness", Scope: rule.CompositeConfiguration},
	})
	if err != nil {
		t.Fatal(err)
	}
	caller, source, composite := &configuration.Config{}, &configuration.Config{}, &configuration.Config{}
	registry := binding.NewRegistry(rules)
	scope := registry.Prepare(caller, source, composite)
	other := binding.NewRegistry(rules).Prepare(caller, source, composite)
	snapshot := registry.Seal()
	if _, ok := snapshot.Lookup(other.Rule("source").ID()); ok {
		t.Fatal("accepted another registry's identity")
	}
	for name, want := range map[string]*configuration.Config{"caller": caller, "source": source, "composite": composite} {
		prepared := scope.Rule(name)
		published, ok := snapshot.Lookup(prepared.ID())
		if !ok || published != prepared || published.Config() != want || published.Policy() != want || published.Scope() != scope {
			t.Fatalf("%s execution and publication did not share the prepared decision", name)
		}
	}
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range 100 {
				prepared := scope.Rule("composite")
				if published, ok := snapshot.Lookup(prepared.ID()); !ok || published != prepared {
					t.Error("sealed binding changed")
				}
			}
		})
	}
	group.Wait()
}

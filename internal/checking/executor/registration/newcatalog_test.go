package registration

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestNativeCatalogOwnsDeclarationsAndRegistrationOrder(t *testing.T) {
	t.Parallel()
	declarations := Declarations()
	if len(declarations) == 0 {
		t.Fatal("native catalog is empty")
	}
	rules, err := repoconfig.NewRuleSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	registrations := Registrations(rules)
	if len(registrations) != len(declarations) {
		t.Fatalf("got %d registrations for %d declarations", len(registrations), len(declarations))
	}
	for i, declaration := range declarations {
		if registrations[i].Name != declaration.Name {
			t.Fatalf("registration %d is %q, want %q", i, registrations[i].Name, declaration.Name)
		}
	}
	for i := range declarations {
		if len(declarations[i].Aliases) == 0 {
			continue
		}
		name := declarations[i].Aliases[0].Name
		declarations[i].Aliases[0].Name = "changed"
		if got := Declarations()[i].Aliases[0].Name; got != name {
			t.Fatalf("caller changed the native catalog alias: got %q, want %q", got, name)
		}
		return
	}
	t.Fatal("catalog has no alias to verify detachment")
}

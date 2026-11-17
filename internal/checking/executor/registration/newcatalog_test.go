package registration

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestNativeCatalogOwnsDeclarationsAndOrder(t *testing.T) {
	t.Parallel()
	declarations := Declarations()
	if len(declarations) == 0 {
		t.Fatal("native catalog is empty")
	}
	if _, err := configuration.NewRuleSet(declarations); err != nil {
		t.Fatal(err)
	}
	if len(All) != len(declarations) {
		t.Fatalf("got %d native rules for %d declarations", len(All), len(declarations))
	}
	for i, declaration := range declarations {
		if All[i].Name != declaration.Name {
			t.Fatalf("native rule %d is %q, want %q", i, All[i].Name, declaration.Name)
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

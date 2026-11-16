package rule

import "testing"

func TestNamesResolveDeclaredAliases(t *testing.T) {
	t.Parallel()
	descriptors := []Descriptor{{Name: "check", Aliases: []Alias{{Name: "shortcut"}, {Name: "old-check", Deprecated: true}}}}
	names, err := NewNames(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"check", "shortcut", "old-check"} {
		if got := names.ResolveAlias(spelling); got != "check" {
			t.Errorf("%s resolves to %s", spelling, got)
		}
	}
	descriptors[0].Aliases[0].Name = "changed"
	if names.ResolveAlias("shortcut") != "check" || names.ResolveAlias("unregistered") != "unregistered" {
		t.Fatal("name index depends on caller-owned data")
	}
	cloned := descriptors[0].Clone()
	cloned.Aliases[0].Name = "mutated"
	if descriptors[0].Aliases[0].Name != "changed" {
		t.Fatal("descriptor clone shares aliases")
	}
}

func TestNamesRejectAmbiguousDeclarations(t *testing.T) {
	t.Parallel()
	for _, declarations := range [][]Descriptor{
		{{Name: "check"}, {Name: "check"}},
		{{Name: "check", Aliases: []Alias{{Name: "check"}}}},
		{{Name: "check", Aliases: []Alias{{Name: "alias"}, {Name: "alias"}}}},
		{{Name: "check", Aliases: []Alias{{Name: "other"}}}, {Name: "other"}},
		{{Name: "check", Aliases: []Alias{{Name: "shared"}}}, {Name: "other", Aliases: []Alias{{Name: "shared"}}}},
		{{Name: "check", Aliases: []Alias{{Name: ""}}}},
		{{Name: "check", Aliases: []Alias{{Name: "bad name"}}}},
	} {
		if _, err := NewNames(declarations); err == nil {
			t.Errorf("ambiguous or invalid names accepted: %+v", declarations)
		}
	}
}

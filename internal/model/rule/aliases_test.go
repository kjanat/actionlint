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
		t.Fatal("name index reflects edits to the descriptors")
	}
	cloned := descriptors[0].Clone()
	cloned.Aliases[0].Name = "mutated"
	if descriptors[0].Aliases[0].Name != "changed" {
		t.Fatal("descriptor clone shares aliases")
	}
}

func TestNamesRestrictKeepsSelectedRulesAndAliases(t *testing.T) {
	t.Parallel()
	names, err := NewNames([]Descriptor{
		{Name: "native", Aliases: []Alias{{Name: "old-native"}}},
		{Name: "tool", Aliases: []Alias{{Name: "old-tool"}}},
		{Name: "other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	native := names.Restrict(func(target string) bool { return target != "tool" })
	for spelling, want := range map[string]bool{"native": true, "old-native": true, "other": true, "tool": false, "old-tool": false, "unknown": false} {
		if got := native.Contains(spelling); got != want {
			t.Errorf("restricted Contains(%q) = %v, want %v", spelling, got, want)
		}
	}
	if native.ResolveAlias("old-native") != "native" || native.ResolveAlias("old-tool") != "old-tool" {
		t.Fatal("restricted index resolves an alias of a rule it excludes")
	}
	if !names.Contains("old-tool") {
		t.Fatal("Restrict changed the original index")
	}
	nested := native.Restrict(func(target string) bool { return target != "other" })
	if nested.Contains("other") || nested.Contains("tool") || !nested.Contains("old-native") {
		t.Fatal("nested restriction does not apply both predicates")
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

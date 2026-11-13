package checking

import "testing"

func TestDeclaredAliasesResolveToTheirOwningRules(t *testing.T) {
	t.Parallel()
	for _, declaration := range ListRules() {
		for _, alias := range declaration.Aliases {
			resolved, found := FindRule(alias.Name)
			if !found || resolved.Name != declaration.Name || ResolveRuleAlias(alias.Name) != declaration.Name {
				t.Errorf("alias %s does not resolve to its declaring rule %s", alias.Name, declaration.Name)
			}
		}
		if len(declaration.Aliases) > 0 {
			declaration.Aliases[0].Name = "changed-by-caller"
			fresh, found := FindRule(declaration.Name)
			if !found || fresh.Aliases[0].Name == "changed-by-caller" {
				t.Fatal("catalogue exposes mutable aliases")
			}
		}
	}
}

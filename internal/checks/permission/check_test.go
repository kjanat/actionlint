package permission

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/diagnostic"
	"github.com/google/go-cmp/cmp"
)

func TestScopesIsACopy(t *testing.T) {
	first := Scopes()
	first["contents"][0] = "mutated"
	delete(first, "issues")
	second := Scopes()
	if second["contents"][0] != "read" || len(second["issues"]) != 3 || len(second) != len(scopes) {
		t.Fatalf("catalogue aliased: %+v", second)
	}
}

func TestCheck(t *testing.T) {
	at := func(line, column int) diagnostic.Position { return diagnostic.Position{Line: line, Column: column} }
	value := func(line, column int, text string) Value { return Value{Text: text, Start: at(line, column)} }
	for _, test := range []struct {
		name  string
		input Declaration
		want  []diagnostic.Diagnostic
	}{
		{"read-all", Declaration{All: new(value(1, 14, "read-all"))}, nil},
		{"write-all", Declaration{All: new(value(1, 14, "write-all"))}, nil},
		{"invalid all", Declaration{All: new(value(1, 14, "all")), Scopes: []Scope{{value(2, 3, "unknown"), value(2, 12, "read")}}}, []diagnostic.Diagnostic{
			{Rule: "permissions", Start: at(1, 14), Message: `"all" is invalid for permission for all the scopes. available values are "read-all", "write-all" or {}`},
		}},
		{"valid scopes", Declaration{Scopes: []Scope{{value(2, 3, "contents"), value(2, 13, "read")}, {value(3, 3, "id-token"), value(3, 13, "write")}, {value(4, 3, "models"), value(4, 11, "none")}}}, nil},
		{"unknown scope", Declaration{Scopes: []Scope{{value(2, 3, "Contents"), value(2, 13, "read")}}}, []diagnostic.Diagnostic{
			{Rule: "permissions", Start: at(2, 3), Message: `unknown permission scope "Contents". all available permission scopes are "actions", "artifact-metadata", "attestations", "checks", "code-quality", "contents", "copilot-requests", "deployments", "discussions", "drives", "id-token", "issues", "models", "packages", "pages", "pull-requests", "repository-projects", "security-events", "statuses", "vulnerability-alerts"`},
		}},
		{"invalid level", Declaration{Scopes: []Scope{{value(2, 3, "contents"), value(2, 13, "execute")}, {value(3, 3, "id-token"), value(3, 13, "read")}}}, []diagnostic.Diagnostic{
			{Rule: "permissions", Start: at(2, 13), Message: `"execute" is invalid as permission of scope "contents". available values are "read", "write", "none"`},
			{Rule: "permissions", Start: at(3, 13), Message: `"read" is invalid as permission of scope "id-token". available values are "write", "none"`},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want, Check(test.input)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
	for name := range scopes {
		if !strings.Contains(Check(Declaration{Scopes: []Scope{{Value{Text: name}, Value{Text: "bogus"}}}})[0].Message, `"bogus" is invalid as permission of scope "`+name+`"`) {
			t.Errorf("scope %s accepted an unknown level", name)
		}
	}
}

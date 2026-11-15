package correctness

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	permissionmodel "actionlint.kjanat.dev/v2/internal/model/workflows/permissions"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestPermissionDiagnostics(t *testing.T) {
	t.Parallel()
	at := func(line, column int) sourcepos.Position { return sourcepos.Position{Line: line, Column: column} }
	value := func(line, column int, text string) permissionValue {
		return permissionValue{Text: text, Start: at(line, column)}
	}
	for _, test := range []struct {
		name  string
		input permissionDeclaration
		want  []results.Diagnostic
	}{
		{"read-all", permissionDeclaration{All: new(value(1, 14, "read-all"))}, nil},
		{"write-all", permissionDeclaration{All: new(value(1, 14, "write-all"))}, nil},
		{"invalid all", permissionDeclaration{All: new(value(1, 14, "all")), Scopes: []permissionScope{{value(2, 3, "unknown"), value(2, 12, "read")}}}, []results.Diagnostic{
			{Start: at(1, 14), Message: `"all" is invalid for permission for all the scopes. available values are "read-all", "write-all" or {}`},
		}},
		{"valid scopes", permissionDeclaration{Scopes: []permissionScope{{value(2, 3, "contents"), value(2, 13, "read")}, {value(3, 3, "id-token"), value(3, 13, "write")}, {value(4, 3, "models"), value(4, 11, "none")}}}, nil},
		{"unknown scope", permissionDeclaration{Scopes: []permissionScope{{value(2, 3, "Contents"), value(2, 13, "read")}}}, []results.Diagnostic{
			{Start: at(2, 3), Message: `unknown permission scope "Contents". all available permission scopes are "actions", "artifact-metadata", "attestations", "checks", "code-quality", "contents", "copilot-requests", "deployments", "discussions", "drives", "id-token", "issues", "models", "packages", "pages", "pull-requests", "repository-projects", "security-events", "statuses", "vulnerability-alerts"`},
		}},
		{"invalid level", permissionDeclaration{Scopes: []permissionScope{{value(2, 3, "contents"), value(2, 13, "execute")}, {value(3, 3, "id-token"), value(3, 13, "read")}}}, []results.Diagnostic{
			{Start: at(2, 13), Message: `"execute" is invalid as permission of scope "contents". available values are "read", "write", "none"`},
			{Start: at(3, 13), Message: `"read" is invalid as permission of scope "id-token". available values are "write", "none"`},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(test.want, permissionDiagnostics(test.input)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
	for name := range permissionmodel.Scopes() {
		if !strings.Contains(permissionDiagnostics(permissionDeclaration{Scopes: []permissionScope{{permissionValue{Text: name}, permissionValue{Text: "bogus"}}}})[0].Message, `"bogus" is invalid as permission of scope "`+name+`"`) {
			t.Errorf("scope %s accepted an unknown level", name)
		}
	}
}

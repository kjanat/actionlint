package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestPropertyDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, want string }{
		{"github.repositoryUsrl", `property "repositoryUsrl" is not defined in github; did you mean "repositoryUrl"?`},
		{"GITHUB['RepositoryUsrl']", `property "RepositoryUsrl" is not defined in GITHUB; did you mean "repositoryUrl"?`},
		{"github.repositoryUrl", ""},
		{"github.REPOSITORYURL", ""},
		{"github['REPOSITORYURL']", ""},
		{"github['event'][0]['author'].displayNme", `property "displayNme" is not defined in github.event[].author for the configured workflow events`},
		{"github.event.*.author.displayNme", `property "displayNme" is not defined in github.event.*.author for the configured workflow events`},
		{"github.event[0].author.displayNamm", `property "displayNamm" is not defined in github.event[].author; did you mean "displayName"?`},
		{"env.ANYTHING", ""},
		{"github.event[0].author[env.KEY]", ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			// displayNme is equally close to two declared names: never guess.
			author := exprtypes.NewNamedObjectType(map[string]exprtypes.Type{"displayName": exprtypes.StringType(), "displayNme2": exprtypes.StringType()})
			gh, _ := contextspec.BuiltinGlobalVariableTypes["github"].Object()
			gh = gh.With("event", exprtypes.NewArrayType(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"author": author.Type()}).Type()).Type())
			checker.SetContextTypes(map[string]exprtypes.Type{"github": gh.Type(), "env": exprtypes.NewMapObjectType(exprtypes.StringType()).Type()})
			checker.SetContextAvailability([]string{"github", "env"})
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(expreval.EvaluateSubexpressions(node))
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatal(findings)
				}
			} else if len(findings) != 1 || findings[0].Message != tc.want {
				t.Fatalf("want %q; got %v", tc.want, findings)
			}
		})
	}
}

func TestCanonicalPropertyNamesSurviveCopies(t *testing.T) {
	t.Parallel()
	original := exprtypes.NewNamedObjectType(map[string]exprtypes.Type{"displayName": exprtypes.StringType()})
	merged, _ := original.Type().Merge(exprtypes.NewNamedObjectType(map[string]exprtypes.Type{"otherField": exprtypes.BoolType()}).Type()).Object()
	for _, object := range []exprtypes.ObjectType{
		original.With("other", exprtypes.BoolType()),
		original.WithMapped(exprtypes.StringType()),
		merged,
	} {
		if object.PropertyName("displayname") != "displayName" {
			t.Fatal(object)
		}
		if renamed := object.WithProperty("DISPLAYNAME", exprtypes.BoolType()); renamed.PropertyName("displayname") != "DISPLAYNAME" {
			t.Fatal(renamed)
		}
		if original.PropertyName("displayname") != "displayName" || object.PropertyName("displayname") != "displayName" {
			t.Fatal("copy mutated original spelling")
		}
	}
}

package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
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
			checker := NewChecker(contexts.DefaultExpressionScope(), nil)
			// displayNme is equally close to two declared names: never guess.
			author := types.NewNamedObjectType(map[string]types.Type{"displayName": types.StringType(), "displayNme2": types.StringType()})
			gh, _ := contexts.BuiltinGlobalVariableTypes["github"].Object()
			gh = gh.With("event", types.NewArrayType(types.NewStrictObjectType(map[string]types.Type{"author": author.Type()}).Type()).Type())
			checker.SetContextTypes(map[string]types.Type{"github": gh.Type(), "env": types.NewMapObjectType(types.StringType()).Type()})
			checker.SetContextAvailability([]string{"github", "env"})
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(evaluate.EvaluateSubexpressions(node))
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
	original := types.NewNamedObjectType(map[string]types.Type{"displayName": types.StringType()})
	merged, _ := original.Type().Merge(types.NewNamedObjectType(map[string]types.Type{"otherField": types.BoolType()}).Type()).Object()
	for _, object := range []types.ObjectType{
		original.With("other", types.BoolType()),
		original.WithMapped(types.StringType()),
		merged,
	} {
		if object.PropertyName("displayname") != "displayName" {
			t.Fatal(object)
		}
		if renamed := object.WithProperty("DISPLAYNAME", types.BoolType()); renamed.PropertyName("displayname") != "DISPLAYNAME" {
			t.Fatal(renamed)
		}
		if original.PropertyName("displayname") != "displayName" || object.PropertyName("displayname") != "displayName" {
			t.Fatal("copy mutated original spelling")
		}
	}
}

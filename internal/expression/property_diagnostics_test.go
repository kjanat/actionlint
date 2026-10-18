package expression

import "testing"

func TestPropertyDiagnostics(t *testing.T) {
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
			checker := NewChecker(false, nil)
			// displayNme is equally close to two declared names: never guess.
			author := newNamedObjectType(map[string]Type{"displayName": StringType{}, "displayNme2": StringType{}})
			gh := BuiltinGlobalVariableTypes["github"].DeepCopy().(*ObjectType)
			gh.Props["event"] = &ArrayType{Elem: NewStrictObjectType(map[string]Type{"author": author})}
			checker.SetContextTypes(map[string]Type{"github": gh, "env": NewMapObjectType(StringType{})})
			checker.SetContextAvailability([]string{"github", "env"})
			node, err := NewParser().Parse(NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := checker.Check(node)
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
	original := newNamedObjectType(map[string]Type{"displayName": StringType{}})
	for _, object := range []*ObjectType{
		original.DeepCopy().(*ObjectType),
		copyEventObject(original),
		original.Merge(newNamedObjectType(map[string]Type{"otherField": BoolType{}})).(*ObjectType),
	} {
		if object.PropertyName("displayname") != "displayName" {
			t.Fatal(object.PropertyNames)
		}
		object.SetProperty("DISPLAYNAME", BoolType{})
		if original.PropertyName("displayname") != "displayName" {
			t.Fatal("copy mutated original spelling")
		}
	}
}

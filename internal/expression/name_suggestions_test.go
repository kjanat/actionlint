package expression

import (
	"maps"
	"strings"
	"testing"
)

func TestCloseExpressionNames(t *testing.T) {
	for _, tc := range []struct {
		input, key, suggestion string
	}{
		{"githbu.ref", "jobs.<job_id>.steps.run", "github"},
		{"gitub.ref", "jobs.<job_id>.steps.run", "github"},
		{"githhub.ref", "jobs.<job_id>.steps.run", "github"},
		{"GITHAB.ref", "jobs.<job_id>.steps.run", "github"},
		{"secret.VALUE", "jobs.<job_id>.steps.run", "secrets"},
		{"secret.VALUE", "jobs.<job_id>.if", ""},
		{"unrelated.ref", "jobs.<job_id>.steps.run", ""},
		{"startWith('abc', 'a')", "jobs.<job_id>.steps.run", "startsWith"},
		{"startssWith('abc', 'a')", "jobs.<job_id>.steps.run", "startsWith"},
		{"STRTSWITH('abc', 'a')", "jobs.<job_id>.steps.run", "startsWith"},
		{"hashFile('*.go')", "jobs.<job_id>.steps.run", "hashFiles"},
		{"hashFile('*.go')", "jobs.<job_id>.if", ""},
		{"alway()", "jobs.<job_id>.steps.if", "always"},
		{"alway()", "jobs.<job_id>.steps.run", ""},
	} {
		t.Run(tc.input+"/"+tc.key, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(false, nil)
			checker.SetWorkflowKeyAvailability(tc.key)
			_, errors := checker.Check(node)
			if len(errors) != 1 {
				t.Fatalf("expected one undefined-name finding, got %v", errors)
			}
			message := errors[0].Message
			if tc.suggestion == "" {
				if strings.Contains(message, "did you mean") {
					t.Fatal(message)
				}
			} else if !strings.Contains(message, `did you mean "`+tc.suggestion+`"?`) {
				t.Fatal(message)
			}
		})
	}
}

func TestAmbiguousExpressionNames(t *testing.T) {
	for _, function := range []bool{false, true} {
		checker := NewChecker(false, nil)
		input := "githab.ref"
		if function {
			checker.funcs = maps.Clone(checker.funcs)
			checker.funcs["startswhth"] = []*FuncSignature{{Name: "startsWhth", Ret: BoolType{}}}
			input = "startsWath()"
		} else {
			checker.ensureVarsCopied()
			checker.vars["githob"] = NewEmptyStrictObjectType()
			checker.SetContextAvailability([]string{"github", "githob"})
		}
		node, err := NewParser().Parse(NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		_, errors := checker.Check(node)
		if len(errors) != 1 || strings.Contains(errors[0].Message, "did you mean") {
			t.Fatalf("ambiguous name must not choose a winner: %v", errors)
		}
	}
}

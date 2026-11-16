package correctness

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestScriptSafeScalarReferences(t *testing.T) {
	t.Parallel()
	checker := typecheck.NewChecker(contexts.DefaultExpressionScope(), nil)
	contexts.MergeInputTypes(checker, types.NewStrictObjectType(map[string]types.Type{"enabled": types.BoolType(), "count": types.NumberType(), "text": types.StringType()}))
	for _, tc := range []struct {
		input string
		safe  bool
	}{
		{"inputs.enabled", true},
		{"inputs['ENABLED']", true},
		{"inputs.count", true},
		{"inputs.text", false},
		{"inputs.missing", false},
		{"env.VALUE", false},
		{"github.ref_protected", true},
		{"toJSON(inputs.enabled)", true},
		{"toJSON(inputs.count)", true},
		{"toJSON(inputs.text)", false},
		{"toJSON(inputs.text == 'value')", true},
		{"toJSON(!inputs.text)", true},
		{"toJSON(toJSON(inputs.enabled))", false},
	} {
		node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if safe := IsScalarInterpolation(node, checker.CheckResult(evaluate.EvaluateSubexpressions(node)).Types); safe != tc.safe {
			t.Errorf("%s: safe=%t, want %t", tc.input, safe, tc.safe)
		}
	}
}

func TestScriptInterpolationValueSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expression string
		typeOf     types.Type
		finding    bool
	}{
		{"env.ARGUMENT", types.StringType(), true},
		{"inputs.value", types.StringType(), true},
		{"steps.build.outputs.value", types.AnyType(), true},
		{"vars.ARGUMENT", types.StringType(), true},
		{"matrix.target", types.StringType(), false},
		{"matrix.unknown", types.StringType(), true},
		{"inputs.count", types.NumberType(), false},
		{"inputs.enabled", types.BoolType(), false},
		{"github", types.ObjectType{}.Type(), false},
		{"fromJSON('[1,2]')", types.NewArrayType(types.NumberType()).Type(), false},
		{"github.repositoryUrl", types.StringType(), false},
		{"github['repositoryUrl']", types.StringType(), false},
		{"github.sha", types.StringType(), false},
		{"github.run_id", types.StringType(), false},
		{"runner.os", types.StringType(), false},
		{"job.status", types.StringType(), false},
		{"steps.test.conclusion", types.StringType(), false},
		{"steps.test.outcome", types.StringType(), false},
		{"steps['TEST']['CONCLUSION']", types.StringType(), false},
		{"steps[inputs.id].outcome", types.StringType(), false},
		{"steps['missing'].outcome", types.StringType(), false},
		{"needs.build.result", types.StringType(), false},
		{"needs['BUILD']['RESULT']", types.StringType(), false},
		{"needs[inputs.job].result", types.StringType(), false},
		{"needs['missing'].result", types.StringType(), false},
		{"steps.test.outputs.conclusion", types.StringType(), true},
		{"needs.build.outputs.result", types.StringType(), true},
		{"steps.test[inputs.property]", types.AnyType(), true},
		{"needs.build.conclusion", types.StringType(), true},
		{"steps.test.result", types.StringType(), true},
		{"github.ref_name", types.StringType(), true},
		{"github.workflow", types.StringType(), true},
		{"github.actor", types.StringType(), true},
		{"'linux-x64'", types.StringType(), false},
		{"'argument with spaces'", types.StringType(), true},
		{"'$(command)'", types.StringType(), true},
		{"'safe; command'", types.StringType(), true},
		{"'safe\"command'", types.StringType(), true},
		{"inputs.enabled && 'linux' || 'darwin'", types.StringType(), false},
		{"inputs.enabled && env.ARGUMENT || 'darwin'", types.StringType(), true},
		{"true && matrix.target", types.StringType(), false},
		{"false && env.ARGUMENT", types.BoolType(), false},
		{"case(inputs.enabled, 'linux', 'darwin')", types.StringType(), false},
		{"case(inputs.enabled, 'linux', env.ARGUMENT)", types.StringType(), true},
		{"case(true, 'linux', env.ARGUMENT)", types.StringType(), false},
		{"hashFiles('**/go.sum')", types.StringType(), false},
		{"toJSON(github.repository == null)", types.StringType(), false},
		{"toJSON(!github.repository)", types.StringType(), false},
		{"toJSON('safe')", types.StringType(), true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := ScriptInterpolation(node, tc.typeOf, typecheck.NewChecker(contexts.DefaultExpressionScope(), nil).CheckResult(evaluate.EvaluateSubexpressions(node)).Types, ShellArgumentInsertion, func(ref syntax.Node) bool {
				name := syntax.StaticReference(ref)
				return name == "matrix.target" || name == "steps.test" || name == "needs.build"
			})
			if (len(findings) != 0) != tc.finding {
				t.Fatalf("got %v; want finding=%t", findings, tc.finding)
			}
			if len(findings) > 0 && (findings[0].Rule != "script-interpolation" || findings[0].Code != "unknown-script-value") {
				t.Fatalf("wrong finding identity: %+v", findings[0])
			}
		})
	}
}

func TestScriptStatusNeedsKnownWordSelector(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expression string
		known      bool
	}{
		{"steps.test.outcome", true},
		{"steps['TEST'].conclusion", true},
		{"needs.build.result", true},
		{"steps['missing'].outcome", false},
		{"needs['missing'].result", false},
		{"steps[inputs.id].outcome", false},
		{"needs[inputs.job].result", false},
		{"steps.test.outputs.value", false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := ScriptInterpolation(node, types.StringType(), typecheck.NewChecker(contexts.DefaultExpressionScope(), nil).CheckResult(evaluate.EvaluateSubexpressions(node)).Types, ShellWordInsertion, func(ref syntax.Node) bool {
				name := syntax.StaticReference(ref)
				return tc.known && (name == "steps.test" || name == "needs.build")
			})
			if (len(findings) == 0) != tc.known {
				t.Fatalf("findings=%v; known selector=%t", findings, tc.known)
			}
		})
	}
	for _, expression := range []string{"steps[inputs.id].outcome", "steps['missing'].outcome"} {
		node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(expression + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		findings := ScriptInterpolation(node, types.StringType(), typecheck.NewChecker(contexts.DefaultExpressionScope(), nil).CheckResult(evaluate.EvaluateSubexpressions(node)).Types, ShellArgumentInsertion, nil)
		if len(findings) != 0 {
			t.Errorf("data argument rejected for %s: %v", expression, findings)
		}
	}
}

func TestSafeScriptLiteral(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "linux-x64", "org/repo", "https://github.com/org/repo", "false", "123"} {
		if !SafeScriptLiteral(value) {
			t.Errorf("safe value rejected: %q", value)
		}
	}
	for _, value := range []string{"a b", "a\nb", "a\tb", "$NAME", "$(cmd)", "`cmd`", "a;b", "a&b", "a|b", "a'b", "a\"b", "a\\b", "%NAME%", "a!b", "*.txt", "a,b", "@args"} {
		if SafeScriptLiteral(value) {
			t.Errorf("unsafe syntax characters accepted: %q", value)
		}
	}
}

func TestScriptKeywordWordProof(t *testing.T) {
	t.Parallel()
	for _, word := range []string{"case", "coproc", "do", "done", "else", "elif", "esac", "fi", "for", "function", "if", "in", "select", "then", "time", "until", "while"} {
		if !SafeScriptLiteral(word) || SafeScriptWord(word) {
			t.Errorf("keyword must retain only its character proof: %q", word)
		}
	}
	for _, word := range []string{"echo", "linux-x64", "org/repo", "fi-linux", "https://github.com/org/repo"} {
		if !SafeScriptWord(word) {
			t.Errorf("ordinary literal lost its word proof: %q", word)
		}
	}
}

func TestNumericScriptLiteralsRequireArgumentPosition(t *testing.T) {
	t.Parallel()
	if !SafeScriptLiteral("") || SafeScriptWord("") {
		t.Error("empty literal must have only an argument proof")
	}
	for _, value := range []string{"0", "2", "123"} {
		if !SafeScriptLiteral(value) || SafeScriptWord(value) {
			t.Errorf("numeric literal must have only an argument proof: %q", value)
		}
	}
	for _, value := range []string{"-2", "2.0", "2f"} {
		if !SafeScriptWord(value) {
			t.Errorf("non-IO-number literal lost its word proof: %q", value)
		}
	}
}

func TestSafeScriptArguments(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "--allow-no-files --diff ", "--excludes .github", "--flag\tvalue", "if fi 2"} {
		if !SafeScriptArguments(text) {
			t.Fatalf("literal argument list rejected: %q", text)
		}
	}
	for _, text := range []string{"a; id", "$(id)", "`id`", "x\ny", "x\ry", "x\u00a0y", "x|y", "x>y", "x<y", "x#y", "x'y", `x"y`, `x\y`, "*.go", "x=y"} {
		if SafeScriptArguments(text) {
			t.Fatalf("shell syntax accepted: %q", text)
		}
	}
}

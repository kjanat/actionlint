package correctness

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestScriptSafeScalarReferences(t *testing.T) {
	t.Parallel()
	checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
	contextspec.MergeInputTypes(checker, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"enabled": exprtypes.BoolType(), "count": exprtypes.NumberType(), "text": exprtypes.StringType()}))
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
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if safe := IsScalarInterpolation(node, checker.CheckResult(expreval.EvaluateSubexpressions(node)).Types); safe != tc.safe {
			t.Errorf("%s: safe=%t, want %t", tc.input, safe, tc.safe)
		}
	}
}

func TestScriptInterpolationValueSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expression string
		typeOf     exprtypes.Type
		finding    bool
	}{
		{"env.ARGUMENT", exprtypes.StringType(), true},
		{"inputs.value", exprtypes.StringType(), true},
		{"steps.build.outputs.value", exprtypes.AnyType(), true},
		{"vars.ARGUMENT", exprtypes.StringType(), true},
		{"matrix.target", exprtypes.StringType(), false},
		{"matrix.unknown", exprtypes.StringType(), true},
		{"inputs.count", exprtypes.NumberType(), false},
		{"inputs.enabled", exprtypes.BoolType(), false},
		{"github", exprtypes.ObjectType{}.Type(), false},
		{"fromJSON('[1,2]')", exprtypes.NewArrayType(exprtypes.NumberType()).Type(), false},
		{"github.repositoryUrl", exprtypes.StringType(), false},
		{"github['repositoryUrl']", exprtypes.StringType(), false},
		{"github.sha", exprtypes.StringType(), false},
		{"github.run_id", exprtypes.StringType(), false},
		{"runner.os", exprtypes.StringType(), false},
		{"job.status", exprtypes.StringType(), false},
		{"steps.test.conclusion", exprtypes.StringType(), false},
		{"steps.test.outcome", exprtypes.StringType(), false},
		{"steps['TEST']['CONCLUSION']", exprtypes.StringType(), false},
		{"steps[inputs.id].outcome", exprtypes.StringType(), false},
		{"steps['missing'].outcome", exprtypes.StringType(), false},
		{"needs.build.result", exprtypes.StringType(), false},
		{"needs['BUILD']['RESULT']", exprtypes.StringType(), false},
		{"needs[inputs.job].result", exprtypes.StringType(), false},
		{"needs['missing'].result", exprtypes.StringType(), false},
		{"steps.test.outputs.conclusion", exprtypes.StringType(), true},
		{"needs.build.outputs.result", exprtypes.StringType(), true},
		{"steps.test[inputs.property]", exprtypes.AnyType(), true},
		{"needs.build.conclusion", exprtypes.StringType(), true},
		{"steps.test.result", exprtypes.StringType(), true},
		{"github.ref_name", exprtypes.StringType(), true},
		{"github.workflow", exprtypes.StringType(), true},
		{"github.actor", exprtypes.StringType(), true},
		{"'linux-x64'", exprtypes.StringType(), false},
		{"'argument with spaces'", exprtypes.StringType(), true},
		{"'$(command)'", exprtypes.StringType(), true},
		{"'safe; command'", exprtypes.StringType(), true},
		{"'safe\"command'", exprtypes.StringType(), true},
		{"inputs.enabled && 'linux' || 'darwin'", exprtypes.StringType(), false},
		{"inputs.enabled && env.ARGUMENT || 'darwin'", exprtypes.StringType(), true},
		{"true && matrix.target", exprtypes.StringType(), false},
		{"false && env.ARGUMENT", exprtypes.BoolType(), false},
		{"case(inputs.enabled, 'linux', 'darwin')", exprtypes.StringType(), false},
		{"case(inputs.enabled, 'linux', env.ARGUMENT)", exprtypes.StringType(), true},
		{"case(true, 'linux', env.ARGUMENT)", exprtypes.StringType(), false},
		{"hashFiles('**/go.sum')", exprtypes.StringType(), false},
		{"toJSON(github.repository == null)", exprtypes.StringType(), false},
		{"toJSON(!github.repository)", exprtypes.StringType(), false},
		{"toJSON('safe')", exprtypes.StringType(), true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := ScriptInterpolation(node, tc.typeOf, exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(node)).Types, ShellArgumentInsertion, func(ref exprsyntax.Node) bool {
				name := exprsyntax.StaticReference(ref)
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
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := ScriptInterpolation(node, exprtypes.StringType(), exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(node)).Types, ShellWordInsertion, func(ref exprsyntax.Node) bool {
				name := exprsyntax.StaticReference(ref)
				return tc.known && (name == "steps.test" || name == "needs.build")
			})
			if (len(findings) == 0) != tc.known {
				t.Fatalf("findings=%v; known selector=%t", findings, tc.known)
			}
		})
	}
	for _, expression := range []string{"steps[inputs.id].outcome", "steps['missing'].outcome"} {
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(expression + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		findings := ScriptInterpolation(node, exprtypes.StringType(), exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(node)).Types, ShellArgumentInsertion, nil)
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

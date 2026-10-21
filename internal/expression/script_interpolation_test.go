package expression

import "testing"

func TestScriptSafeScalarReferences(t *testing.T) {
	checker := NewChecker(false, nil)
	checker.UpdateInputs(NewStrictObjectType(map[string]Type{"enabled": BoolType{}, "count": NumberType{}, "text": StringType{}}))
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
		node, err := NewParser().Parse(NewLexer(tc.input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if safe := checker.IsScriptScalar(node); safe != tc.safe {
			t.Errorf("%s: safe=%t, want %t", tc.input, safe, tc.safe)
		}
	}
}

func TestScriptInterpolationValueSafety(t *testing.T) {
	for _, tc := range []struct {
		expression string
		typeOf     Type
		finding    bool
	}{
		{"env.ARGUMENT", StringType{}, true},
		{"inputs.value", StringType{}, true},
		{"steps.build.outputs.value", AnyType{}, true},
		{"vars.ARGUMENT", StringType{}, true},
		{"matrix.target", StringType{}, false},
		{"matrix.unknown", StringType{}, true},
		{"inputs.count", NumberType{}, false},
		{"inputs.enabled", BoolType{}, false},
		{"github", &ObjectType{}, false},
		{"fromJSON('[1,2]')", &ArrayType{Elem: NumberType{}}, false},
		{"github.repositoryUrl", StringType{}, false},
		{"github['repositoryUrl']", StringType{}, false},
		{"github.sha", StringType{}, false},
		{"github.run_id", StringType{}, false},
		{"runner.os", StringType{}, false},
		{"job.status", StringType{}, false},
		{"steps.test.conclusion", StringType{}, false},
		{"steps.test.outcome", StringType{}, false},
		{"steps['TEST']['CONCLUSION']", StringType{}, false},
		{"steps[inputs.id].outcome", StringType{}, false},
		{"needs.build.result", StringType{}, false},
		{"needs['BUILD']['RESULT']", StringType{}, false},
		{"needs[inputs.job].result", StringType{}, false},
		{"steps.test.outputs.conclusion", StringType{}, true},
		{"needs.build.outputs.result", StringType{}, true},
		{"steps.test[inputs.property]", AnyType{}, true},
		{"needs.build.conclusion", StringType{}, true},
		{"steps.test.result", StringType{}, true},
		{"github.ref_name", StringType{}, true},
		{"github.workflow", StringType{}, true},
		{"github.actor", StringType{}, true},
		{"'linux-x64'", StringType{}, false},
		{"'argument with spaces'", StringType{}, true},
		{"'$(command)'", StringType{}, true},
		{"'safe; command'", StringType{}, true},
		{"'safe\"command'", StringType{}, true},
		{"inputs.enabled && 'linux' || 'darwin'", StringType{}, false},
		{"inputs.enabled && env.ARGUMENT || 'darwin'", StringType{}, true},
		{"true && matrix.target", StringType{}, false},
		{"false && env.ARGUMENT", BoolType{}, false},
		{"case(inputs.enabled, 'linux', 'darwin')", StringType{}, false},
		{"case(inputs.enabled, 'linux', env.ARGUMENT)", StringType{}, true},
		{"case(true, 'linux', env.ARGUMENT)", StringType{}, false},
		{"hashFiles('**/go.sum')", StringType{}, false},
		{"toJSON(github.repository == null)", StringType{}, false},
		{"toJSON(!github.repository)", StringType{}, false},
		{"toJSON('safe')", StringType{}, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := NewChecker(false, nil).CheckScriptInterpolation(node, tc.typeOf, true, func(ref Node) bool {
				name := eventReference(ref)
				return name == "matrix.target"
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

func TestSafeScriptLiteral(t *testing.T) {
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
	for _, word := range []string{"case", "coproc", "do", "done", "else", "elif", "esac", "fi", "for", "function", "if", "in", "select", "then", "time", "until", "while"} {
		if !SafeScriptLiteral(word) || SafeScriptWord(word) {
			t.Errorf("keyword must retain only its character proof: %q", word)
		}
	}
	for _, word := range []string{"", "echo", "linux-x64", "org/repo", "fi-linux", "https://github.com/org/repo"} {
		if !SafeScriptWord(word) {
			t.Errorf("ordinary literal lost its word proof: %q", word)
		}
	}
}

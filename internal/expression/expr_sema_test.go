package expression

import (
	"fmt"
	"maps"
	"strings"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"

	contextspec "actionlint.kjanat.dev/v2/internal/expression/context"
)

func TestExprContextPropertySuggestions(t *testing.T) {
	for _, tc := range []struct {
		input, key, suggestion string
	}{
		{"github_repository", "jobs.<job_id>.if", "github.repository"},
		{"GITHUB_REPOSITORY", "jobs.<job_id>.if", "github.repository"},
		{"github.job_workflow_sha", "jobs.<job_id>.steps.run", "job.workflow_sha"},
		{"github.job_workflow_sha", "jobs.<job_id>.if", ""},
		{"github_nonexistent", "jobs.<job_id>.if", ""},
		{"secrets_anything", "jobs.<job_id>.steps.run", ""},
		{"github.repository", "jobs.<job_id>.steps.run", ""},
	} {
		t.Run(tc.input+"/"+tc.key, func(t *testing.T) {
			expr, err := NewParser().Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(false, nil)
			checker.SetWorkflowKeyAvailability(tc.key)
			_, errs := checker.Check(expr)
			var messages []string
			for _, err := range errs {
				messages = append(messages, err.Message)
			}
			message := strings.Join(messages, "\n")
			if tc.suggestion == "" {
				if strings.Contains(message, "did you mean") {
					t.Fatal(message)
				}
			} else if len(errs) != 1 || !strings.Contains(message, fmt.Sprintf("did you mean %q?", tc.suggestion)) {
				t.Fatalf("expected suggestion %q, got %q", tc.suggestion, message)
			}
		})
	}
}

func TestExprContextSuggestionsPreserveUserProperties(t *testing.T) {
	for _, receiver := range []string{"inputs", "matrix", "steps", "needs", "jobs", "env", "custom"} {
		for _, property := range []string{"github_token", "runner_os", "job_workflow_sha"} {
			t.Run(receiver+"."+property, func(t *testing.T) {
				checker := NewChecker(false, nil)
				checker.ensureVarsCopied()
				checker.vars[receiver] = NewStrictObjectType(map[string]Type{"declared": BoolType()}).Type()
				checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.run")
				checker.availableContexts = append(checker.availableContexts, receiver)
				expr, err := NewParser().Parse(NewLexer(receiver + "." + property + "}}"))
				if err != nil {
					t.Fatal(err)
				}
				_, errs := checker.Check(expr)
				want := fmt.Sprintf("property %q is not defined in %s", property, receiver)
				if len(errs) != 1 || errs[0].Message != want {
					t.Fatalf("got %v, want %q", errs, want)
				}
				checker.vars[receiver] = NewStrictObjectType(map[string]Type{property: StringType()}).Type()
				ty, errs := checker.Check(expr)
				if len(errs) != 0 || ty.String() != "string" {
					t.Fatalf("declared property: type=%s errors=%v", ty.String(), errs)
				}
			})
		}
	}
}

func TestExprDeclaredInputSuggestions(t *testing.T) {
	for _, tc := range []struct {
		name, expression, key, suggestion string
		inputs                            ObjectType
	}{
		{"declared input wins", "github_repository", "jobs.<job_id>.steps.run", "inputs.github_repository", NewStrictObjectType(map[string]Type{"github_repository": StringType()})},
		{"case insensitive", "GITHUB_REPOSITORY", "jobs.<job_id>.if", "inputs.github_repository", NewStrictObjectType(map[string]Type{"github_repository": StringType()})},
		{"ordinary input", "deploy_target", "jobs.<job_id>.steps.run", "inputs.deploy_target", NewStrictObjectType(map[string]Type{"deploy_target": StringType()})},
		{"undeclared input", "github_repository", "jobs.<job_id>.steps.run", "github.repository", NewEmptyStrictObjectType()},
		{"loose input object", "github_repository", "jobs.<job_id>.steps.run", "github.repository", NewObjectType(map[string]Type{"github_repository": StringType()})},
		{"inputs unavailable", "github_repository", "", "github.repository", NewStrictObjectType(map[string]Type{"github_repository": StringType()})},
		{"dotted context unchanged", "github.job_workflow_sha", "jobs.<job_id>.steps.run", "job.workflow_sha", NewStrictObjectType(map[string]Type{"job_workflow_sha": StringType()})},
		{"declared dotted input", "inputs.github_repository", "jobs.<job_id>.steps.run", "", NewStrictObjectType(map[string]Type{"github_repository": StringType()})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := NewChecker(false, nil)
			checker.UpdateInputs(tc.inputs)
			checker.SetWorkflowKeyAvailability(tc.key)
			if tc.key == "" {
				checker.SetContextAvailability([]string{"github"})
			}
			expr, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, errs := checker.Check(expr)
			if tc.suggestion == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, fmt.Sprintf("did you mean %q?", tc.suggestion)) {
				t.Fatalf("expected suggestion %q, got %v", tc.suggestion, errs)
			}
		})
	}
}

func TestExprBareFunctionNames(t *testing.T) {
	for _, key := range []string{"jobs.<job_id>.if", "jobs.<job_id>.steps.if"} {
		for _, tc := range []struct {
			input, want string
		}{
			{"always", `function "always" must be called with parentheses; did you mean "always()"?`},
			{"success", `function "success" must be called with parentheses; did you mean "success()"?`},
			{"failure", `function "failure" must be called with parentheses; did you mean "failure()"?`},
			{"cancelled", `function "cancelled" must be called with parentheses; did you mean "cancelled()"?`},
			{"AlWaYs", `function "AlWaYs" must be called with parentheses; did you mean "AlWaYs()"?`},
			{"contains", `function "contains" must be called with parentheses and arguments`},
			{"fromJSON", `function "fromJSON" must be called with parentheses and arguments`},
			{"hashFiles", `function "hashFiles" must be called with parentheses and arguments`},
			{"format", `function "format" must be called with parentheses and arguments`},
			{"unknown", `undefined variable "unknown". available variables are `},
			{"always()", ""},
			{"success()", ""},
			{"failure()", ""},
			{"cancelled()", ""},
			{"inputs.always", ""},
		} {
			t.Run(key+"/"+tc.input, func(t *testing.T) {
				expr, err := NewParser().Parse(NewLexer(tc.input + "}}"))
				if err != nil {
					t.Fatal(err)
				}
				checker := NewChecker(false, nil)
				checker.UpdateInputs(NewStrictObjectType(map[string]Type{"always": BoolType()}))
				checker.SetWorkflowKeyAvailability(key)
				_, errs := checker.Check(expr)
				if tc.want == "" {
					if len(errs) != 0 {
						t.Fatalf("unexpected diagnostics: %v", errs)
					}
					return
				}
				if len(errs) != 1 || !strings.HasPrefix(errs[0].Message, tc.want) {
					t.Fatalf("got %v, want %q", errs, tc.want)
				}
				if errs[0].Line != 1 || errs[0].Column != 1 {
					t.Fatalf("unexpected position: %+v", errs[0])
				}
			})
		}
	}
	t.Run("variable takes precedence over function", func(t *testing.T) {
		expr, err := NewParser().Parse(NewLexer("always}}"))
		if err != nil {
			t.Fatal(err)
		}
		checker := NewChecker(false, nil)
		checker.vars = maps.Clone(checker.vars)
		checker.vars["always"] = BoolType()
		checker.SetContextAvailability([]string{"always"})
		ty, errs := checker.Check(expr)
		if len(errs) != 0 || !cmp.Equal(ty, BoolType()) {
			t.Fatalf("got %v, %v; want bool without diagnostics", ty, errs)
		}
	})
}

func TestExprSemanticsCheckOK(t *testing.T) {
	testCases := []struct {
		what          string
		input         string
		expected      Type
		funcs         map[string][]*FuncSignature
		matrix        ObjectType
		steps         ObjectType
		needs         ObjectType
		inputs        ObjectType
		secrets       ObjectType
		jobs          ObjectType
		availContexts []string
		availSPFuncs  []string
		configVars    []string
		configSecrets []string
	}{
		{
			what:     "null",
			input:    "null",
			expected: NullType(),
		},
		{
			what:     "bool",
			input:    "true",
			expected: BoolType(),
		},
		{
			what:     "integer",
			input:    "42",
			expected: NumberType(),
		},
		{
			what:     "float",
			input:    "-3.14e16",
			expected: NumberType(),
		},
		{
			what:     "string",
			input:    "'this is string'",
			expected: StringType(),
		},
		{
			what:     "variable",
			input:    "github",
			expected: BuiltinGlobalVariableTypes["github"],
		},
		{
			what:     "object property dereference",
			input:    "test().bar.piyo",
			expected: BoolType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"bar": NewObjectType(map[string]Type{
								"piyo": BoolType(),
							}).Type(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "object property dereference of global variable",
			input:    "job.container.network",
			expected: StringType(),
		},
		{
			what:     "job context workflow_repository property",
			input:    "job.workflow_repository",
			expected: StringType(),
		},
		{
			what:     "job context workflow_ref property",
			input:    "job.workflow_ref",
			expected: StringType(),
		},
		{
			what:     "job context workflow_sha property",
			input:    "job.workflow_sha",
			expected: StringType(),
		},
		{
			what:     "job context workflow_file_path property",
			input:    "job.workflow_file_path",
			expected: StringType(),
		},
		{
			what:     "github context artifacts property",
			input:    "github.artifacts",
			expected: StringType(),
		},
		{
			what:     "github context artifacts_list property",
			input:    "github.artifacts_list",
			expected: StringType(),
		},
		{
			what:     "github context retention_days property",
			input:    "github.retention_days",
			expected: StringType(),
		},
		{
			what:     "object property dereference for any type",
			input:    "github.event.labels",
			expected: AnyType(),
		},
		{
			what:     "array element dereference",
			input:    "test().bar.*",
			expected: newFilteredArrayType(BoolType()).Type(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"bar": NewArrayType(BoolType()).Type(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "filter object property by array element dereference",
			input:    "test().foo.*.bar.piyo",
			expected: newFilteredArrayType(StringType()).Type(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"foo": NewArrayType(
								NewObjectType(map[string]Type{
									"bar": NewObjectType(map[string]Type{
										"piyo": StringType(),
									}).Type(),
								}).Type(),
							).Type(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "filter strict object property by array element dereference",
			input:    "test().foo.*.bar.piyo",
			expected: newFilteredArrayType(StringType()).Type(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewStrictObjectType(map[string]Type{
							"foo": NewArrayType(
								NewStrictObjectType(map[string]Type{
									"bar": NewStrictObjectType(map[string]Type{
										"piyo": StringType(),
									}).Type(),
								}).Type(),
							).Type(),
						}).Type(),
					},
				},
			},
		},
		// TODO: Add strictprops
		{
			what:     "array element dereference with any type",
			input:    "github.event.labels.*.name",
			expected: newFilteredArrayType(AnyType()).Type(),
		},
		{
			what:     "nested array element dereference",
			input:    "github.event.issues.*.labels.*.name",
			expected: newFilteredArrayType(AnyType()).Type(),
		},
		{
			what:     "array element dereference with object type",
			input:    "github.*.name",
			expected: newFilteredArrayType(AnyType()).Type(),
		},
		{
			what:     "function call",
			input:    "contains('hello', 'll')",
			expected: BoolType(),
		},
		{
			what:     "function call overload",
			input:    "contains(github.event.labels, 'foo')",
			expected: BoolType(),
		},
		{
			what:     "function call zero arguments",
			input:    "always()",
			expected: BoolType(),
		},
		{
			what:     "function call variable length parameters",
			input:    "format('hello {0} {1}', 42, true)",
			expected: StringType(),
		},
		{
			what:     "object property index access",
			input:    "test()['bar']['piyo']",
			expected: BoolType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"bar": NewObjectType(map[string]Type{
								"piyo": BoolType(),
							}).Type(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "object property index access with any type",
			input:    "github.event['FOOO']",
			expected: AnyType(),
		},
		{
			what:     "array element dereference",
			input:    "test()[0]",
			expected: BoolType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret:  NewArrayType(BoolType()).Type(),
					},
				},
			},
		},
		{
			what:     "array element index access with any type fallback",
			input:    "github.event.labels[0]",
			expected: AnyType(),
		},
		{
			what:     "index access to dereferenced array",
			input:    "test().foo.*.bar[0]",
			expected: StringType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"foo": NewArrayType(
								NewObjectType(map[string]Type{
									"bar": StringType(),
								}).Type(),
							).Type(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "coerce array dereference into array at function parameter",
			input:    "contains(test().*.x, 10)",
			expected: BoolType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewArrayType(
							NewObjectType(map[string]Type{
								"x": NumberType(),
							}).Type(),
						).Type(),
					},
				},
				"contains": BuiltinFuncSignatures["contains"],
			},
		},
		{
			what:     "index access to dereferenced array with any type fallback",
			input:    "github.event.labels.*.name[0]",
			expected: AnyType(),
		},
		{
			what:     "! operator",
			input:    "!true",
			expected: BoolType(),
		},
		{
			what:     "! operator with non-bool operand",
			input:    "!null",
			expected: BoolType(),
		},
		{
			what:     "< operator",
			input:    "0 < 1",
			expected: BoolType(),
		},
		{
			what:     "<= operator",
			input:    "0 <= 1",
			expected: BoolType(),
		},
		{
			what:     "> operator",
			input:    "0 > 1",
			expected: BoolType(),
		},
		{
			what:     ">= operator",
			input:    "0 >= 1",
			expected: BoolType(),
		},
		{
			what:     "== operator",
			input:    "0 == 1",
			expected: BoolType(),
		},
		{
			what:     "!= operator",
			input:    "0 != 1",
			expected: BoolType(),
		},
		{
			what:     "&& operator",
			input:    "true && false",
			expected: BoolType(),
		},
		{
			what:     "|| operator",
			input:    "true || false",
			expected: BoolType(),
		},
		{
			what:     "&& operator with non-bool operands",
			input:    "10 && 'foo'",
			expected: StringType(),
		},
		{
			what:     "|| operator with non-bool operands",
			input:    "'foo' || 42",
			expected: StringType(),
		},
		{
			what:  "coercing two objects on && operator",
			input: "foo() && bar()",
			expected: NewStrictObjectType(map[string]Type{
				"foo": NumberType(),
				"bar": BoolType(),
			}).Type(),
			funcs: map[string][]*FuncSignature{
				"foo": {
					{
						Name: "foo",
						Ret: NewStrictObjectType(map[string]Type{
							"foo": NumberType(),
						}).Type(),
					},
				},
				"bar": {
					{
						Name: "bar",
						Ret: NewStrictObjectType(map[string]Type{
							"bar": BoolType(),
						}).Type(),
					},
				},
			},
		},
		{
			what:  "coercing two objects on || operator",
			input: "foo() || bar()",
			expected: NewStrictObjectType(map[string]Type{
				"foo": NumberType(),
				"bar": BoolType(),
			}).Type(),
			funcs: map[string][]*FuncSignature{
				"foo": {
					{
						Name: "foo",
						Ret: NewStrictObjectType(map[string]Type{
							"foo": NumberType(),
						}).Type(),
					},
				},
				"bar": {
					{
						Name: "bar",
						Ret: NewStrictObjectType(map[string]Type{
							"bar": BoolType(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "== operator with loose equality check",
			input:    "true == 1.1",
			expected: BoolType(),
		},
		{
			what:     "arguments of format() is not checked when first argument is not a literal",
			input:    "format(github.action, 1, 2, 3)",
			expected: StringType(),
		},
		{
			what:     "matrix value with typed matrix values",
			input:    "matrix.foooo",
			expected: StringType(),
			matrix: NewStrictObjectType(map[string]Type{
				"foooo": StringType(),
			}),
		},
		{
			what:     "step output value with typed steps outputs",
			input:    "steps.foo.outputs",
			expected: NewEmptyObjectType().Type(),
			steps: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs":    NewEmptyObjectType().Type(),
					"conclusion": StringType(),
					"outcome":    StringType(),
				}).Type(),
			}),
		},
		{
			what:     "step conclusion with typed steps outputs",
			input:    "steps.foo.conclusion",
			expected: StringType(),
			steps: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs":    NewEmptyObjectType().Type(),
					"conclusion": StringType(),
					"outcome":    StringType(),
				}).Type(),
			}),
		},
		{
			what:     "needs context object",
			input:    "needs",
			expected: NewEmptyStrictObjectType().Type(),
		},
		{
			what:     "output string in needs context object",
			input:    "needs.foo.outputs.out1",
			expected: StringType(),
			needs: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs": NewStrictObjectType(map[string]Type{
						"out1": StringType(),
						"out2": StringType(),
					}).Type(),
					"result": StringType(),
				}).Type(),
			}),
		},
		{
			what:     "result in needs context object",
			input:    "needs.foo.result",
			expected: StringType(),
			needs: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs": NewStrictObjectType(map[string]Type{
						"out1": StringType(),
						"out2": StringType(),
					}).Type(),
					"result": StringType(),
				}).Type(),
			}),
		},
		{
			what:     "number is coerced into string",
			input:    "startsWith('42foo', 42)",
			expected: BoolType(),
		},
		{
			what:     "string is coerced into bool",
			input:    "!'hello'",
			expected: BoolType(),
		},
		{
			what:     "coerce number into bool",
			input:    "!42",
			expected: BoolType(),
		},
		{
			what:     "coerce null into bool",
			input:    "!null",
			expected: BoolType(),
		},
		{
			what:     "coerce string into bool",
			input:    "!'hello'",
			expected: BoolType(),
		},
		{
			what:     "case insensitive comparison for object property",
			input:    "test().foo-Bar_PIYO",
			expected: NullType(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewObjectType(map[string]Type{
							"foo-bar_piyo": NullType(),
						}).Type(),
					},
				},
			},
		},
		{
			what:     "case insensitive comparison for function name",
			input:    "toJSON(fromjson(toJson(github)))",
			expected: StringType(),
		},
		{
			what:     "case insensitive comparison for context name",
			input:    "JOB.CONTAINER.NETWORK",
			expected: StringType(),
		},
		{
			what:     "format() function arguments validation",
			input:    "format('{0}{0}{0} {1}{2}{1} {1}{2}{1}{2} {0} {1}{1}{1} {2}{2}{2} {0}{0}{0}{0} {0}', 1, 'foo', true)",
			expected: StringType(),
		},
		{
			what:     "braces not for placeholders in format string of format() call",
			input:    "format('{0} {} {x} {', 1)",
			expected: StringType(),
		},
		{
			what:     "map object dereference",
			input:    "env.FOO",
			expected: StringType(),
		},
		{
			what:     "map object dreference on array filter",
			input:    "test().*.foo",
			expected: newFilteredArrayType(NumberType()).Type(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewArrayType(
							NewMapObjectType(NumberType()).Type(),
						).Type(),
					},
				},
			},
		},
		{
			what:     "map object dreference on object filter",
			input:    "test().*.foo",
			expected: newFilteredArrayType(NumberType()).Type(),
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewMapObjectType(
							NewMapObjectType(NumberType()).Type(),
						).Type(),
					},
				},
			},
		},
		{
			what:     "loose object at object filter",
			input:    "github.event.*.foo",
			expected: newFilteredArrayType(AnyType()).Type(),
		},
		{
			what:     "strict object which has at least one object element at object filter",
			input:    "github.*.foo",
			expected: newFilteredArrayType(AnyType()).Type(),
		},
		{
			what:     "map object index access with string literal",
			input:    "env['FOO']",
			expected: StringType(),
		},
		{
			what:     "map object index access with dynamic value",
			input:    "env[github.action]",
			expected: StringType(),
		},
		{
			what:     "nested object in map object",
			input:    "job.services.my_service.network",
			expected: StringType(),
		},
		{
			what:     "narrowed inputs object",
			input:    "inputs.hello",
			expected: NumberType(),
			inputs: NewStrictObjectType(map[string]Type{
				"hello": NumberType(),
			}),
		},
		{
			what:     "narrowed secrets object",
			input:    "secrets.token",
			expected: StringType(),
			secrets: NewStrictObjectType(map[string]Type{
				"token": StringType(),
			}),
		},
		{
			what:     "default secrets object",
			input:    "secrets.any_value",
			expected: StringType(),
		},
		{
			what:     "automatically supplied secret",
			input:    "secrets.github_token",
			expected: StringType(),
			secrets: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
		},
		{
			what:     "automatically supplied secret",
			input:    "secrets.ACTIONS_STEP_DEBUG",
			expected: StringType(),
			secrets: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
		},
		{
			what:     "automatically supplied secret",
			input:    "secrets.ACTIONS_RUNNER_DEBUG",
			expected: StringType(),
			secrets: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
		},
		{
			what:     "jobs object",
			input:    "jobs.some_job",
			expected: NewEmptyObjectType().Type(),
			jobs: NewStrictObjectType(map[string]Type{
				"some_job": NewEmptyObjectType().Type(),
			}),
		},
		{
			what:          "available context",
			input:         "strategy.fail-fast",
			expected:      BoolType(),
			availContexts: []string{"strategy"},
		},
		{
			what:          "available contexts",
			input:         "strategy.fail-fast",
			expected:      BoolType(),
			availContexts: []string{"matrix", "strategy"},
		},
		{
			what:         "special function",
			input:        "success()",
			expected:     BoolType(),
			availSPFuncs: []string{"success"},
		},
		{
			what:         "non-special function",
			input:        "contains('hello, world', 'o, w')",
			expected:     BoolType(),
			availSPFuncs: []string{"always"},
		},
		{
			what:         "case-insensitive special function name",
			input:        "hashFiles('aaa.txt')",
			expected:     StringType(),
			availSPFuncs: []string{"hashfiles"},
		},
		{
			what:     "configuration variable",
			input:    "vars.SOME_VARIABLE",
			expected: StringType(),
		},
		{
			what:       "known configuration variable",
			input:      "vars.SOME_VARIABLE",
			expected:   StringType(),
			configVars: []string{"SOME_VARIABLE"},
		},
		{
			what:       "configuration variable name is case insensitive",
			input:      "vars.SOME_VARIABLE",
			expected:   StringType(),
			configVars: []string{"some_variable"},
		},
		{
			what:     "secret without config",
			input:    "secrets.MY_SECRET",
			expected: StringType(),
		},
		{
			what:          "known secret",
			input:         "secrets.MY_SECRET",
			expected:      StringType(),
			configSecrets: []string{"MY_SECRET"},
		},
		{
			what:          "secret name is case insensitive",
			input:         "secrets.MY_SECRET",
			expected:      StringType(),
			configSecrets: []string{"My_Secret"},
		},
		{
			what:          "known secret through index access",
			input:         "secrets['MY_SECRET']",
			expected:      StringType(),
			configSecrets: []string{"MY_SECRET"},
		},
		{
			what:       "known configuration variable through index access",
			input:      "vars['SOME_VARIABLE']",
			expected:   StringType(),
			configVars: []string{"SOME_VARIABLE"},
		},
		{
			what:          "builtin secret with allowlist",
			input:         "secrets.GITHUB_TOKEN",
			expected:      StringType(),
			configSecrets: []string{"MY_SECRET"},
		},
		{
			what:          "builtin debug secret with empty allowlist",
			input:         "secrets.ACTIONS_STEP_DEBUG",
			expected:      StringType(),
			configSecrets: []string{},
		},
		{
			what:          "declared secret is not checked against the allowlist",
			input:         "secrets.FOO",
			expected:      StringType(),
			secrets:       NewStrictObjectType(map[string]Type{"foo": StringType()}).WithMapped(StringType()),
			configSecrets: []string{"BAR"},
		},
		{
			what:     "open secrets type still allows unknown names without config",
			input:    "secrets.UNKNOWN_SECRET",
			expected: StringType(),
			secrets:  NewStrictObjectType(map[string]Type{"foo": StringType()}).WithMapped(StringType()),
		},
		{
			what:     "narrow type of && operator by assumed value (#384)",
			input:    "('foo' && 10) || 20",
			expected: NumberType(),
		},
		{
			what:     "narrow type of || operator by assumed value (#384)",
			input:    "('foo' || 10) && 20",
			expected: NumberType(),
		},
		{
			what:     "narrow type of nested && operator",
			input:    "((('foo' && true) || false) && 10) || 20",
			expected: NumberType(),
		},
		{
			what:     "narrow type of nested || operator",
			input:    "((('foo' || true) && false) || 10) && 20",
			expected: NumberType(),
		},
		{
			what:     "don't narrow type on nested && operator",
			input:    "('foo' && 10) && 20",
			expected: StringType(),
		},
		{
			what:     "don't narrow type on nested || operator",
			input:    "('foo' || 10) || 20",
			expected: StringType(),
		},
		{
			what:     "narrowed || operator at LHS and && operator at RHS",
			input:    "('foo' || 10) && (('foo' && 10) || 20)",
			expected: NumberType(),
		},
		{
			what:     "narrowed && operator at LHS and || operator at RHS",
			input:    "('foo' && 10) || (('foo' || 10) && 20)",
			expected: NumberType(),
		},
		{
			what:     "not operator negates && operator type narrowing",
			input:    "!('foo' && 10) && 20",
			expected: NumberType(),
		},
		{
			what:     "not operator negates || operator type narrowing",
			input:    "!('foo' || 10) || 20",
			expected: NumberType(),
		},
		{
			what:     "double not operators does nothing on type narrowing",
			input:    "!!('foo' || 10) && 20",
			expected: NumberType(),
		},
		{
			what:     "escaped braces in format string",
			input:    "format('hello {{1}} {0}', 42)",
			expected: StringType(),
		},
		{
			what:     "format specifier is escaped",
			input:    "format('hello {{{0}', 'world')", // First {{ is escaped. {0} is not escaped
			expected: StringType(),
		},
		{
			what:  "fromJSON with JSON constant value",
			input: `fromJSON('{"foo":true,"bar":["foo", 12.3],"piyo":null}')`,
			expected: NewStrictObjectType(map[string]Type{
				"foo":  BoolType(),
				"bar":  NewArrayType(StringType()).Type(), // Element type was merged
				"piyo": NullType(),
			}).Type(),
		},
		{
			what:     "case() with single predicate",
			input:    "case(true, 'a', 'b')",
			expected: StringType(),
		},
		{
			what:     "case() with multiple predicates",
			input:    "case(true, 1, false, 2, 3)",
			expected: NumberType(),
		},
		{
			what:     "case() with numeric comparison predicate",
			input:    "case(1 == 1, 'a', 'b')",
			expected: StringType(),
		},
		{
			what:     "case() with string comparison predicate",
			input:    "case('yes' != '', 'a', 'b')",
			expected: StringType(),
		},
		{
			what:     "case() with null comparison predicate",
			input:    "case(null == null, 'a', 'b')",
			expected: StringType(),
		},
		{
			what:     "case() case insensitivity",
			input:    "CASE(true, 1, 2)",
			expected: NumberType(),
		},
		{
			what:     "case() with expression as predicate",
			input:    "case(startsWith('hello', 'he'), 'yes', 'no')",
			expected: StringType(),
		},
		{
			what:     "case() with many predicates",
			input:    "case(false, 'a', false, 'b', false, 'c', true, 'd', 'default')",
			expected: StringType(),
		},
		{
			what:     "case() with boolean results",
			input:    "case(github.event_name == 'push', true, false)",
			expected: BoolType(),
		},
		{
			what:     "case() with null results",
			input:    "case(github.event_name == 'push', null, null)",
			expected: NullType(),
		},
		{
			what:     "case() includes incompatible default",
			input:    "case(github.event_name == 'push', 1, false)",
			expected: AnyType(),
		},
		{
			what:     "case() includes incompatible middle branch",
			input:    "case(github.event_name == 'push', 1, github.event_name == 'pull_request', null, 2)",
			expected: AnyType(),
		},
		{
			what:     "case() merges string and number results",
			input:    "case(github.event_name == 'push', 1, 'fallback')",
			expected: StringType(),
		},
		{
			what:     "case() unknown default",
			input:    "case(github.event_name == 'push', 'known', fromJSON(env.DATA))",
			expected: AnyType(),
		},
		{
			what:     "case() unknown branch",
			input:    "case(github.event_name == 'push', fromJSON(env.DATA), 'known')",
			expected: AnyType(),
		},
		{
			what:     "case() unknown predicate does not affect result type",
			input:    "case(fromJSON(env.DATA), 'yes', 'no')",
			expected: StringType(),
		},
		{
			what:     "case() nested results",
			input:    "case(github.event_name == 'push', case(github.event_name == 'pull_request', 1, 2), 3)",
			expected: NumberType(),
		},
		{
			what:     "case() merges array elements",
			input:    `case(github.event_name == 'push', fromJSON('[1,2]'), fromJSON('[3]'))`,
			expected: NewArrayType(NumberType()).Type(),
		},
		{
			what:     "case() merges incompatible array elements",
			input:    `case(github.event_name == 'push', fromJSON('[1]'), fromJSON('[false]'))`,
			expected: NewArrayType(AnyType()).Type(),
		},
		{
			what:  "case() merges object properties",
			input: `case(github.event_name == 'push', fromJSON('{"count":1,"name":"one"}'), fromJSON('{"count":2,"enabled":true}'))`,
			expected: NewStrictObjectType(map[string]Type{
				"count": NumberType(), "name": StringType(), "enabled": BoolType(),
			}).Type(),
		},
		{
			what:     "case() object property inference",
			input:    `case(github.event_name == 'push', fromJSON('{"count":1}'), fromJSON('{"count":2}')).count`,
			expected: NumberType(),
		},
		{
			what:     "case() object and null results",
			input:    `case(github.event_name == 'push', fromJSON('{"count":1}'), null)`,
			expected: AnyType(),
		},
		{
			what:     "case() constant true predicate narrows to its branch",
			input:    "case(true, 1, 'unreachable')",
			expected: NumberType(),
		},
		{
			what:     "case() constant false predicate drops its branch",
			input:    "case(false, 'unreachable', 1)",
			expected: NumberType(),
		},
		{
			what:     "case() constant true predicate drops unreachable fromJSON default",
			input:    `case(true, fromJSON('[1,2]'), fromJSON('[false]'))`,
			expected: NewArrayType(NumberType()).Type(),
		},
		{
			what:     "case() constant predicates keep later reachable branches",
			input:    "case(false, 'a', github.event_name == 'push', 1, 2)",
			expected: NumberType(),
		},
	}

	allSPFuncs := []string{}
	for f := range contextspec.SpecialFunctionNames {
		allSPFuncs = append(allSPFuncs, f)
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			p := NewParser()
			e, err := p.Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal("Parse error:", tc.input)
			}

			c := NewChecker(false, &Options{Variables: tc.configVars, Secrets: tc.configSecrets})
			c.SetContextAvailability([]string{"github", "job", "jobs", "matrix", "steps", "needs", "env", "inputs", "secrets", "vars", "runner"})
			if tc.funcs != nil {
				c.funcs = tc.funcs
			}
			// A zero ObjectType in the table means the context is not replaced.
			if tc.matrix != (ObjectType{}) {
				c.UpdateMatrix(tc.matrix)
			}
			if tc.steps != (ObjectType{}) {
				c.UpdateSteps(tc.steps)
			}
			if tc.needs != (ObjectType{}) {
				c.UpdateNeeds(tc.needs)
			}
			if tc.inputs != (ObjectType{}) {
				c.UpdateInputs(tc.inputs)
			}
			if tc.secrets != (ObjectType{}) {
				c.UpdateSecrets(tc.secrets)
			}
			if tc.jobs != (ObjectType{}) {
				c.UpdateJobs(tc.jobs)
			}
			if len(tc.availContexts) > 0 {
				c.SetContextAvailability(tc.availContexts)
			}
			if len(tc.availSPFuncs) > 0 {
				c.SetSpecialFunctionAvailability(tc.availSPFuncs)
			} else {
				c.SetSpecialFunctionAvailability(allSPFuncs)
			}
			ty, errs := c.Check(e)
			if len(errs) > 0 {
				t.Fatal("semantics check failed:", errs)
			}

			if diff := cmp.Diff(tc.expected, ty); diff != "" {
				t.Fatalf(`wanted: %s
but got:%s
diff:
%s`, tc.expected.String(), ty.String(), diff)
			}
		})
	}
}

func TestExprSemanticsCheckError(t *testing.T) {
	testCases := []struct {
		what          string
		input         string
		expected      []string
		funcs         map[string][]*FuncSignature
		matrix        ObjectType
		steps         ObjectType
		needs         ObjectType
		secrets       ObjectType
		availCtx      []string
		availSP       []string
		configVars    []string
		configSecrets []string
	}{
		{
			what:  "undefined variable",
			input: "fooooo",
			expected: []string{
				"undefined variable \"fooooo\"",
			},
		},
		{
			what:  "receiver of object dereference is not an object",
			input: "true.foo",
			expected: []string{
				"receiver of object dereference \"foo\" must be type of object but got \"bool\"",
			},
		},
		{
			what:  "strict prop check",
			input: "github.foo",
			expected: []string{
				"property \"foo\" is not defined in github",
			},
		},
		{
			what:  "case() checks each argument once",
			input: "case(github.missing_predicate, github.missing_value, github.missing_default)",
			expected: []string{
				"property \"missing_predicate\" is not defined in github",
				"property \"missing_value\" is not defined in github",
				"property \"missing_default\" is not defined in github",
			},
		},
		{
			what:  "strict prop check at object property filter for array dereference",
			input: "test().*.bar",
			expected: []string{
				"property \"bar\" is not defined in this object",
			},
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret: NewArrayType(
							NewStrictObjectType(map[string]Type{
								"foo": BoolType(),
							}).Type(),
						).Type(),
					},
				},
			},
		},
		{
			what:  "array element is not object for filtering array dereference",
			input: "test().*.bar",
			expected: []string{
				"property filtered by \"bar\" at object filtering must be type of object but got \"string\"",
			},
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret:  NewArrayType(StringType()).Type(),
					},
				},
			},
		},
		{
			what:  "receiver of array dereference is not an array",
			input: "true.*",
			expected: []string{
				"receiver of object filtering `.*` must be type of array or object but got \"bool\"",
			},
		},
		{
			what:  "receiver of object filter is not an object which has object element",
			input: "env.*",
			expected: []string{
				"elements of object at receiver of object filtering `.*` must be type of object but got \"string\". the type of receiver was \"{string => string}\"",
			},
		},
		{
			what:  "receiver of object filter is an object which has no object element",
			input: "runner.*",
			expected: []string{
				"cannot be filtered by object filtering `.*` since it has no object element",
			},
		},
		{
			what:  "index access to invalid value (number)",
			input: "true[0]",
			expected: []string{
				"index access operand must be type of object or array but got \"bool\"",
			},
		},
		{
			what:  "index access to invalid value (string)",
			input: "true['hello']",
			expected: []string{
				"index access operand must be type of object or array but got \"bool\"",
			},
		},
		{
			what:  "index access to array with not a number",
			input: "test()['hi']",
			expected: []string{
				"index access of array must be type of number but got \"string\"",
			},
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret:  NewArrayType(StringType()).Type(),
					},
				},
			},
		},
		{
			what:  "index access to array dereference with not a number",
			input: "test().*['hi']",
			expected: []string{
				"index access of array must be type of number but got \"string\"",
			},
			funcs: map[string][]*FuncSignature{
				"test": {
					{
						Name: "test",
						Ret:  NewArrayType(StringType()).Type(),
					},
				},
			},
		},
		{
			what:  "index access to object with not a string",
			input: "env[0]",
			expected: []string{
				"property access of object must be type of string but got \"number\"",
			},
		},
		{
			what:  "strict prop check with string literal index access to object",
			input: "github['fooooo']",
			expected: []string{
				"property \"fooooo\" is not defined in github",
			},
		},
		{
			what:  "undefined function",
			input: "foooo()",
			expected: []string{
				"undefined function \"foooo\"",
			},
		},
		{
			what:  "wrong number of arguments at function call",
			input: "contains('foo')",
			expected: []string{
				"number of arguments is wrong. function \"contains(string, string) -> bool\" takes 2 parameters but 1 arguments are given",
				"number of arguments is wrong. function \"contains(array<any>, any) -> bool\" takes 2 parameters but 1 arguments are given",
			},
		},
		{
			what:  "wrong number of arguments at function call for variable length parameters",
			input: "hashFiles()",
			expected: []string{
				"number of arguments is wrong. function \"hashFiles(string...) -> string\" takes at least 1 parameters but 0 arguments are given",
			},
		},
		{
			what:  "wrong type at parameter",
			input: "startsWith('foo', null)",
			expected: []string{
				"2nd argument of function call is not assignable. \"null\" cannot be assigned to \"string\"",
			},
		},
		{
			what:  "wrong type at parameter of overloaded function",
			input: "contains('foo', null)",
			expected: []string{
				"2nd argument of function call is not assignable. \"null\" cannot be assigned to \"string\"",
				"1st argument of function call is not assignable. \"string\" cannot be assigned to \"array<any>\"",
			},
		},
		{
			what:  "wrong type at rest parameter",
			input: "hashFiles(null)",
			expected: []string{
				"1st argument of function call is not assignable. \"null\" cannot be assigned to \"string\"",
			},
		},
		{
			what:  "wrong type at rest parameter part2",
			input: "hashFiles('foo', null)",
			expected: []string{
				"2nd argument of function call is not assignable. \"null\" cannot be assigned to \"string\"",
			},
		},
		{
			what:  "less arguments for format() builtin function call",
			input: "format('format {0} {1}', 'foo')",
			expected: []string{
				"format string \"format {0} {1}\" contains placeholder {1} but only 1 arguments are given to format",
			},
		},
		{
			what:  "more arguments for format() builtin function call",
			input: "format('format {0} {1} {2}', 'foo', 1, true, null)",
			expected: []string{
				"format string \"format {0} {1} {2}\" does not contain placeholder {3}. remove argument which is unused in the format string",
			},
		},
		{
			what:  "unused placeholder in format string of format()",
			input: "format('format {0} {2}', 1, 2, 3)",
			expected: []string{
				"format string \"format {0} {2}\" does not contain placeholder {1}. remove argument which is unused in the format string",
			},
		},
		{
			what:  "missing placeholder and less argument at the same time in format string of format()",
			input: "format('format {0} {2}', 1, 2)",
			expected: []string{
				"format string \"format {0} {2}\" does not contain placeholder {1}. remove argument which is unused in the format string",
				"format string \"format {0} {2}\" contains placeholder {2} but only 2 arguments are given to format",
			},
		},
		{
			what:  "missing argument for format() placeholder",
			input: "format('{0}')",
			expected: []string{
				"contains placeholder {0} but only 0 arguments are given",
			},
		},
		{
			what:  "function name of format() call check is case insensitive",
			input: "Format('{0}', 1, 2)",
			expected: []string{
				`format string "{0}" does not contain placeholder {1}`,
			},
		},
		{
			what:  "format specifier is escaped",
			input: "format('hello {{0}}', 'world')",
			expected: []string{
				"does not contain placeholder {0}",
			},
		},
		{
			what:  "format specifier is still escaped",
			input: "format('hello {{{{0}}', 'world')", // First {{ is escaped. {{0}} is still escaped
			expected: []string{
				"does not contain placeholder {0}",
			},
		},
		{
			what:  "undefined matrix value",
			input: "matrix.bar",
			expected: []string{
				"property \"bar\" is not defined in matrix",
			},
			matrix: NewStrictObjectType(map[string]Type{
				"foo": AnyType(),
			}),
		},
		{
			what:  "type mismatch in matrix value",
			input: "startsWith('hello', matrix.foo)",
			expected: []string{
				"2nd argument of function call is not assignable. \"null\" cannot be assigned to \"string\"",
			},
			matrix: NewStrictObjectType(map[string]Type{
				"foo": NullType(),
			}),
		},
		{
			what:  "matrix value with untyped matrix values",
			input: "matrix.foooo",
			expected: []string{
				"property \"foooo\" is not defined in matrix",
			},
		},
		{
			what:  "undefined step id",
			input: "steps.foo",
			expected: []string{
				"property \"foo\" is not defined in steps",
			},
			steps: NewStrictObjectType(map[string]Type{
				"bar": NewStrictObjectType(map[string]Type{
					"outputs":    NewEmptyObjectType().Type(),
					"conclusion": StringType(),
					"outcome":    StringType(),
				}).Type(),
			}),
		},
		{
			what:  "invalid property in step object",
			input: "steps.bar.foo",
			expected: []string{
				"property \"foo\" is not defined in steps.bar",
			},
			steps: NewStrictObjectType(map[string]Type{
				"bar": NewStrictObjectType(map[string]Type{
					"outputs":    NewEmptyObjectType().Type(),
					"conclusion": StringType(),
					"outcome":    StringType(),
				}).Type(),
			}),
		},
		{
			what:  "step output value without typed steps outputs",
			input: "steps.foo.outputs",
			expected: []string{
				"property \"foo\" is not defined in steps",
			},
		},
		{
			what:  "undefined job id in needs context",
			input: "needs.bar",
			expected: []string{
				"property \"bar\" is not defined in needs",
			},
			needs: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs": NewStrictObjectType(map[string]Type{
						"out1": StringType(),
						"out2": StringType(),
					}).Type(),
					"result": StringType(),
				}).Type(),
			}),
		},
		{
			what:  "undefined output in needs context",
			input: "needs.foo.outputs.out3",
			expected: []string{
				"property \"out3\" is not defined in needs.foo.outputs",
			},
			needs: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs": NewStrictObjectType(map[string]Type{
						"out1": StringType(),
						"out2": StringType(),
					}).Type(),
					"result": StringType(),
				}).Type(),
			}),
		},
		{
			what:  "undefined prop in needs context",
			input: "needs.foo.bar",
			expected: []string{
				"property \"bar\" is not defined in needs.foo",
			},
			needs: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"outputs": NewStrictObjectType(map[string]Type{
						"out1": StringType(),
						"out2": StringType(),
					}).Type(),
					"result": StringType(),
				}).Type(),
			}),
		},
		{
			what:  "undefined prop in untyped needs context",
			input: "needs.foo",
			expected: []string{
				"property \"foo\" is not defined in needs",
			},
		},
		{
			what:  "bool literal in upper case",
			input: "TRUE",
			expected: []string{
				"undefined variable \"TRUE\"",
			},
		},
		{
			what:  "null literal in upper case",
			input: "NULL",
			expected: []string{
				"undefined variable \"NULL\"",
			},
		},
		{
			what:  "inputs context accepts nothing by default",
			input: "inputs.hello",
			expected: []string{
				"property \"hello\" is not defined in inputs",
			},
		},
		{
			what:  "available context",
			input: "env.FOO",
			expected: []string{
				"context \"env\" is not allowed here. available context is \"github\"",
			},
			availCtx: []string{"github"},
		},
		{
			what:  "available contexts",
			input: "github.event.labels",
			expected: []string{
				"context \"github\" is not allowed here. available contexts are \"env\", \"matrix\"",
			},
			availCtx: []string{"env", "matrix"},
		},
		{
			what:  "no available context",
			input: "github",
			expected: []string{
				"context \"github\" is not allowed here. no context is available",
			},
			availCtx: []string{},
		},
		{
			what:  "no special function allowed",
			input: "success()",
			expected: []string{
				"calling function \"success\" is not allowed here. \"success\" is only available in ",
			},
			availSP: []string{},
		},
		{
			what:  "special function",
			input: "always()",
			expected: []string{
				"calling function \"always\" is not allowed here. \"always\" is only available in ",
			},
			availSP: []string{"fail", "success"},
		},
		{
			what:  "case-insensitive special function name",
			input: "hashFiles('aaa.txt')",
			expected: []string{
				"calling function \"hashFiles\" is not allowed here. \"hashFiles\" is only available in ",
			},
			availSP: []string{"fail"},
		},
		{
			what:  "no configuration variable is allowed",
			input: "vars.UNKNOWN_VARIABLE",
			expected: []string{
				"no configuration variable is allowed since the variables list is empty",
			},
			configVars: []string{},
		},
		{
			what:  "unknown configuration variable",
			input: "vars.UNKNOWN_VARIABLE",
			expected: []string{
				"undefined configuration variable \"unknown_variable\".",
			},
			configVars: []string{"FOO_BAR"},
		},
		{
			what:  "no secret is allowed",
			input: "secrets.UNKNOWN_SECRET",
			expected: []string{
				"no secret is allowed since the secrets list is empty in actionlint.yaml. you may forget adding the secret \"unknown_secret\" to the list",
			},
			configSecrets: []string{},
		},
		{
			what:  "unknown secret",
			input: "secrets.UNKNOWN_SECRET",
			expected: []string{
				"undefined secret \"unknown_secret\". defined secrets in actionlint.yaml are \"MY_SECRET\"",
			},
			configSecrets: []string{"MY_SECRET"},
		},
		{
			what:  "unknown secret through index access",
			input: "secrets['UNKNOWN_SECRET']",
			expected: []string{
				"undefined secret \"unknown_secret\". defined secrets in actionlint.yaml are \"MY_SECRET\"",
			},
			configSecrets: []string{"MY_SECRET"},
		},
		{
			what:  "unknown configuration variable through index access",
			input: "vars['UNKNOWN_VARIABLE']",
			expected: []string{
				"undefined configuration variable \"unknown_variable\".",
			},
			configVars: []string{"FOO_BAR"},
		},
		{
			what:  "declared secrets stay strict for a workflow_call-only workflow",
			input: "secrets.BAR",
			expected: []string{
				"property \"BAR\" is not defined in secrets",
			},
			secrets:       NewStrictObjectType(map[string]Type{"foo": StringType()}),
			configSecrets: []string{"BAR"},
		},
		{
			what:  "config variable naming convention",
			input: "vars.FOO-BAR",
			expected: []string{
				"configuration variable name \"foo-bar\" can only contain alphabets, decimal numbers, and '_'.",
			},
		},
		{
			what:  "config variable name cannot start with GITHUB_",
			input: "vars.GITHUB_FOOOOOOOO",
			expected: []string{
				"must not start with the GITHUB_ prefix",
			},
		},
		{
			what:  "broken JSON value at fromJSON argument",
			input: `fromJSON('{"foo": true')`,
			expected: []string{
				"broken JSON string is passed to fromJSON() at offset 12",
			},
		},
		{
			what:  "case() with too few arguments",
			input: "case() || case(true) || case(true, 'a')",
			expected: []string{
				"at least 3 parameters but 0 arguments are given",
				"at least 3 parameters but 1 arguments are given",
				"at least 3 parameters but 2 arguments are given",
			},
		},
		{
			what:  "case() with even number of arguments",
			input: "case(true, 'a', false, 'b') || case(true, 'a', false, 'b', true, 'c')",
			expected: []string{
				"case() requires an odd number of arguments (pred/value pairs + default) but got 4",
				"case() requires an odd number of arguments (pred/value pairs + default) but got 6",
			},
		},
	}

	allSP := []string{}
	for f := range contextspec.SpecialFunctionNames {
		allSP = append(allSP, f)
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			p := NewParser()
			e, err := p.Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal("Parse error:", tc.input)
			}

			c := NewChecker(false, &Options{Variables: tc.configVars, Secrets: tc.configSecrets})
			if tc.funcs != nil {
				c.funcs = tc.funcs // Set functions for testing
			}
			// A zero ObjectType in the table means the context is not replaced.
			if tc.matrix != (ObjectType{}) {
				c.UpdateMatrix(tc.matrix)
			}
			if tc.steps != (ObjectType{}) {
				c.UpdateSteps(tc.steps)
			}
			if tc.needs != (ObjectType{}) {
				c.UpdateNeeds(tc.needs)
			}
			if tc.secrets != (ObjectType{}) {
				c.UpdateSecrets(tc.secrets)
			}
			if tc.availCtx != nil {
				c.SetContextAvailability(tc.availCtx)
			} else {
				c.SetContextAvailability([]string{"github", "job", "jobs", "matrix", "steps", "needs", "env", "inputs", "secrets", "vars", "runner"})
			}
			if tc.availSP != nil {
				c.SetSpecialFunctionAvailability(tc.availSP)
			} else {
				c.SetSpecialFunctionAvailability(allSP)
			}

			_, errs := c.Check(e)
			if len(errs) != len(tc.expected) {
				t.Fatalf("semantics check should report %d errors but got %d errors: %v", len(tc.expected), len(errs), errs)
			}
		LoopErrs:
			for _, err := range errs {
				for _, want := range tc.expected {
					if strings.Contains(err.Error(), want) {
						continue LoopErrs
					}
				}
				t.Fatalf("error %q did not match any expected error messages %#v", err.Error(), tc.expected)
			}
		})
	}
}

func TestExprCompareOperandsCheck(t *testing.T) {
	// Matrix of operator -> lhs type -> rhs type -> result
	// Result `true` means the comparison is allowed. `false` means the comparison causes an error.
	matrix := map[string]map[string]map[string]bool{
		"==": {
			"number": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   true,
				"null":   true,
				"object": false,
				"array":  false,
			},
			"string": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   true,
				"null":   true,
				"object": false,
				"array":  false,
			},
			"bool": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   true,
				"null":   true,
				"object": false,
				"array":  false,
			},
			"null": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   true,
				"null":   true,
				"object": true,
				"array":  true,
			},
			"object": {
				"any":    true,
				"number": false,
				"string": false,
				"bool":   false,
				"null":   true,
				"object": true,
				"array":  false,
			},
			"array": {
				"any":      true,
				"number":   false,
				"string":   false,
				"bool":     false,
				"null":     true,
				"object":   false,
				"array":    true,
				"array_2d": false,
			},
			"array_2d": {
				"any":      true,
				"number":   false,
				"string":   false,
				"bool":     false,
				"null":     true,
				"object":   false,
				"array":    false,
				"array_2d": true,
			},
			"any": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   true,
				"null":   true,
				"object": true,
				"array":  true,
			},
		},
		"<": {
			"number": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"string": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"bool": {
				"any":    false,
				"number": false,
				"string": false,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"null": {
				"any":    false,
				"number": false,
				"string": false,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"any": {
				"any":    true,
				"number": true,
				"string": true,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"object": {
				"any":    false,
				"number": false,
				"string": false,
				"bool":   false,
				"null":   false,
				"object": false,
				"array":  false,
			},
			"array": {
				"any":      false,
				"number":   false,
				"string":   false,
				"bool":     false,
				"null":     false,
				"object":   false,
				"array":    false,
				"array_2d": false,
			},
		},
	}

	for op, expr := range matrix {
		for lhs, rest := range expr {
			for rhs, ok := range rest {
				input := lhs + " " + op + " " + rhs
				t.Run(input, func(t *testing.T) {
					p := NewParser()
					e, err := p.Parse(NewLexer(input + "}}"))
					if err != nil {
						t.Fatal("Parse error:", input)
					}

					c := NewChecker(false, nil)
					c.vars = map[string]Type{
						"number":   NumberType(),
						"string":   StringType(),
						"bool":     BoolType(),
						"object":   NewEmptyObjectType().Type(),
						"array":    NewArrayType(NumberType()).Type(),
						"array_2d": NewArrayType(NewArrayType(NumberType()).Type()).Type(),
						"any":      AnyType(),
					}
					c.SetContextAvailability([]string{
						"any",
						"number",
						"string",
						"bool",
						"null",
						"object",
						"array",
						"array_2d",
					})

					ty, errs := c.Check(e)
					if ok {
						if len(errs) > 0 {
							t.Fatal("semantics check failed:", errs)
						}
						if ty.Kind() != KindBool {
							t.Fatalf("wanted bool type but have %q", ty)
						}
					} else {
						if len(errs) != 1 {
							t.Fatal("more than one error occurred", errs)
						}
						msg := errs[0].Error()

						toType := func(n string) string {
							switch n {
							case "array":
								return "array<number>"
							case "array_2d":
								return "array<array<number>>"
							default:
								return n
							}
						}
						want := fmt.Sprintf("%q value cannot be compared to %q value with %q operator", toType(lhs), toType(rhs), op)

						if !strings.Contains(msg, want) {
							t.Fatalf("error message %q doesn't contain expected message %q", msg, want)
						}
					}
				})
			}
		}
	}
}

func TestExprBuiltinFunctionSignatures(t *testing.T) {
	for name, sigs := range BuiltinFuncSignatures {
		if len(sigs) == 0 {
			t.Errorf("overload candidates of %q should not be empty", name)
		}
		{
			ok := true
			for _, r := range name {
				if !unicode.IsLower(r) {
					ok = false
					break
				}
			}
			if !ok {
				t.Errorf("name of function must be in lower case to check in case insensitive: %q", name)
			}
		}
		for i, sig := range sigs {
			if name != strings.ToLower(sig.Name) {
				t.Errorf("name of %dth overload is different from its key: name=%q vs key=%q", i+1, sig.Name, name)
			}
			if sig.VariableLengthParams && len(sig.Params) == 0 {
				t.Errorf("number of arguments of %dth overload of %q must not be empty because VariableLengthParams is set to true", i+1, name)
			}
		}
	}
}

func TestExprSemanticsCheckerUpdateMatrix(t *testing.T) {
	c := NewChecker(false, nil)
	ty := NewEmptyObjectType()
	prev := c.vars["matrix"]
	c.UpdateMatrix(ty)
	if c.vars["matrix"] == prev {
		t.Fatalf("Global variables map was not copied")
	}
	prev = c.vars["matrix"]
	c.UpdateMatrix(ty)
	if c.vars["matrix"] != prev {
		t.Fatalf("Global variables map was copied when calling UpdateMatrix again")
	}
}

func TestExprSemanticsCheckerUpdateSteps(t *testing.T) {
	c := NewChecker(false, nil)
	ty := NewEmptyObjectType()
	prev := c.vars["steps"]
	c.UpdateSteps(ty)
	if c.vars["steps"] == prev {
		t.Fatalf("Global variables map was not copied")
	}
	prev = c.vars["steps"]
	c.UpdateSteps(ty)
	if c.vars["steps"] != prev {
		t.Fatalf("Global variables map was copied when calling UpdateSteps again")
	}
}

func TestExprSematincsCheckerUpdateDispatchInputsVarType(t *testing.T) {
	ty := NewStrictObjectType(map[string]Type{"foo": NullType()})
	c := NewChecker(false, nil)
	c.UpdateDispatchInputs(ty)
	o := objectPath(t, c.vars["github"], "event", "inputs")
	if _, ok := o.Property("foo"); !ok {
		t.Error("Local github.event.inputs is not updated", o)
	}
	if !o.IsStrict() {
		t.Error("Local github.event.inputs is not strict")
	}
	for n, ty := range o.Properties() {
		if ty.Kind() != KindString {
			t.Errorf("Type of %q input is not string: %s", n, ty)
		}
	}

	// Check global value is not polluted
	o = objectPath(t, BuiltinGlobalVariableTypes["github"], "event")
	if _, ok := o.Property("inputs"); ok {
		t.Error("Global github.event.inputs exists", o)
	}
}

// objectPath follows object properties from ty and fails the test when a step
// is not an object.
func objectPath(t *testing.T, ty Type, keys ...string) ObjectType {
	t.Helper()
	object, ok := ty.Object()
	if !ok {
		t.Fatalf("%s is not an object", ty)
	}
	for _, key := range keys {
		next, _ := object.Property(key)
		if object, ok = next.Object(); !ok {
			t.Fatalf("property %q is not an object: %s", key, next)
		}
	}
	return object
}

func TestExprSemanticsCheckerUpdateInputsMultipleTimes(t *testing.T) {
	tests := []struct {
		first  ObjectType
		second ObjectType
		want   ObjectType
	}{
		{
			first: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
			second: NewStrictObjectType(map[string]Type{
				"bar": NumberType(),
			}),
			want: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
				"bar": NumberType(),
			}),
		},
		{
			first: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
			second: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
			want: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}),
		},
		{
			first: NewStrictObjectType(map[string]Type{
				"foo": BoolType(),
			}),
			second: NewStrictObjectType(map[string]Type{
				"foo": NumberType(),
			}),
			want: NewStrictObjectType(map[string]Type{
				"foo": AnyType(),
			}),
		},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("%v then %v", tc.first, tc.second)
		t.Run(name, func(t *testing.T) {
			c := NewChecker(false, nil)
			c.UpdateInputs(tc.first)
			c.UpdateDispatchInputs(tc.second)
			have := c.vars["inputs"]
			if diff := cmp.Diff(have, tc.want.Type()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func testObjectPropertiesAreInLowerCase(t *testing.T, ty Type) {
	t.Helper()
	if object, ok := ty.Object(); ok {
		for n, ty := range object.Properties() {
			for _, r := range n {
				if 'A' <= r && r <= 'Z' {
					t.Errorf("Property of object must not contain uppercase character because comparison is case insensitive but got %q", n)
					break
				}
			}
			testObjectPropertiesAreInLowerCase(t, ty)
		}
	}
	if array, ok := ty.Array(); ok {
		testObjectPropertiesAreInLowerCase(t, array.Elem())
	}
}

func TestBuiltinGlobalVariableTypesValidation(t *testing.T) {
	for ctx, ty := range BuiltinGlobalVariableTypes {
		for _, r := range ctx {
			if 'A' <= r && r <= 'Z' {
				t.Errorf("Context name must not contain uppercase character because comparison is case insensitive but got %q", ctx)
				break
			}
		}
		testObjectPropertiesAreInLowerCase(t, ty)
	}
}

func TestParseFormatSpecifiers(t *testing.T) {
	tests := []struct {
		what string
		in   string
		want []int // Specifiers in the `in` string
	}{
		{
			what: "empty input",
			in:   "",
		},
		{
			what: "no specifier",
			in:   "hello, world!",
		},
		{
			what: "single specifier",
			in:   "Hello{0}specifier",
			want: []int{0},
		},
		{
			what: "multiple specifiers",
			in:   "{0} {1}{2}x{3}_{4}!",
			want: []int{0, 1, 2, 3, 4},
		},
		{
			what: "many specifiers",
			in:   "{0}{1}{2}{3}{4}{5}{6}{7}{8}{9}{10}!",
			want: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			what: "unordered",
			in:   "{2}foo{4} {0}{3}",
			want: []int{2, 4, 0, 3},
		},
		{
			what: "uncontiguous",
			in:   "{0} {2}foo{42} {1}",
			want: []int{0, 2, 42, 1},
		},
		{
			what: "unclosed",
			in:   "{12foo",
		},
		{
			what: "missing closing brace",
			in:   "{0",
		},
		{
			what: "not digit",
			in:   "{hello}",
		},
		{
			what: "space in digits",
			in:   "{1 2}",
		},
		{
			what: "empty",
			in:   "{}",
		},
		{
			what: "opening brace only",
			in:   "{",
		},
		{
			what: "specifier inside specifier",
			in:   "{1{0}2}",
			want: []int{0},
		},
		{
			what: "escaped single",
			in:   "{{0}}",
		},
		{
			what: "complicated escaped format",
			in:   "{{hello{{0}{{{{1}world}}",
		},
		{
			what: "placeholders inside escape",
			in:   "{{{0} {1} {2}}}",
			want: []int{0, 1, 2},
		},
		{
			what: "no extra brace",
			in:   "{0}",
			want: []int{0},
		},
		{
			what: "closing brace 1",
			in:   "{0}}",
		},
		{
			what: "closing brace 2",
			in:   "{0}}}",
			want: []int{0},
		},
		{
			what: "closing brace 3",
			in:   "{0}}}}",
		},
		{
			what: "closing brace 4",
			in:   "{0}}}}}",
			want: []int{0},
		},
		{
			what: "closing brace 1 with trailing character",
			in:   "{0}}x",
		},
		{
			what: "closing brace 2 with trailing character",
			in:   "{0}}}x",
			want: []int{0},
		},
		{
			what: "closing brace 3 with trailing character",
			in:   "{0}}}}x",
		},
		{
			what: "closing brace 4 with trailing character",
			in:   "{0}}}}}x",
			want: []int{0},
		},
		{
			what: "opening brace 1",
			in:   "{{0}",
		},
		{
			what: "opening brace 2",
			in:   "{{{0}",
			want: []int{0},
		},
		{
			what: "opening brace 3",
			in:   "{{{{0}",
		},
		{
			what: "opening brace 4",
			in:   "{{{{{0}",
			want: []int{0},
		},
		{
			what: "opening brace after escaped closing brace",
			in:   "{1}}{2}",
			want: []int{2},
		},
		{
			what: "escaped opening brace after escaped closing brace",
			in:   "{1}}{{2}",
		},
		{
			what: "escaped opening brace after closing brace",
			in:   "{1}{{2}",
			want: []int{1},
		},
		{
			what: "kuma-",
			in:   "{・{ᴥ}・}",
		},
		{
			what: "multi byte",
			in:   "あ{0}い{1}う{2}え{3}お",
			want: []int{0, 1, 2, 3},
		},
		{
			what: "emoji",
			in:   "🐶{0}👨‍👩‍👦‍👦{1}🇺🇸{2}🐦‍🔥",
			want: []int{0, 1, 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			want := map[int]struct{}{}
			for _, i := range tc.want {
				want[i] = struct{}{}
			}
			have := parseFormatFuncSpecifiers(tc.in, len(tc.want))

			if diff := cmp.Diff(want, have); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestExprIsConstant(t *testing.T) {
	tests := []struct {
		input   string
		isConst bool
	}{
		{"true", true},
		{"42", true},
		{"3.14", true},
		{"null", true},
		{"'hello'", true},
		{"foo", false},
		{"foo.bar", false},
		{"foo.*", false},
		{"foo[0]", false},
		{"!true", true},
		{"!null", true},
		{"!foo", false},
		{"1 == 2", true},
		{"'foo' != 'bar'", true},
		{"foo == 2", false},
		{"1 == foo", false},
		{"true && 42 || 13", true},
		{"foo && 42 || 13", false},
		{"true && foo || 13", false},
		{"true && 42 || foo", false},
		{"true && ('foo' == 'bar') && null", true},
		{"false || (3.14 != null) || !null", true},
		{"startsWith('foo', 'f')", true},
		{"startsWith(foo, 'f')", false},
		{"format('{0}{1}', 1.0, true)", true},
		{"format('{0}{1}', startsWith('foo', 'f'), 1.0 == !null)", true},
		{"toJSON(true)", true},
		{"toJSON(foo)", false},
		{"fromJSON('false')", true},
		{"fromJSON(foo)", false},
		{"success()", false},
		{"failure()", false},
		{"hashFiles('foo', 'bar')", false},
		{"conains(fromJSON('[\"foo\"]'), 'foo')", false},
		{"unknownFunc(1, 2, 3)", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			p := NewParser()
			e, err := p.Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatalf("Parse error for input %q: %s", tc.input, err)
			}

			c := NewChecker(false, nil)
			b := c.IsConstant(e)
			if b != tc.isConst {
				t.Fatalf("wanted %v but got %v for input %q", tc.isConst, b, tc.input)
			}
		})
	}
}

// vim: nofoldenable

func TestContextReplacementDispatchInputs(t *testing.T) {
	githubContexts := []struct {
		ty      Type
		present bool
	}{
		{},
		{StringType(), true},
		{NewStrictObjectType(map[string]Type{"event": NewStrictObjectType(nil).Type()}).Type(), true},
	}
	for _, github := range githubContexts {
		checker := NewChecker(false, nil)
		contexts := map[string]Type{}
		if github.present {
			contexts["GITHUB"] = github.ty
		}
		checker.SetContextTypes(contexts)
		checker.UpdateDispatchInputs(NewStrictObjectType(map[string]Type{"enabled": BoolType()}))
		if inputs, ok := checker.vars["inputs"].Object(); !ok {
			t.Fatal("dispatch inputs were not installed")
		} else if _, ok := inputs.Property("enabled"); !ok {
			t.Fatal("dispatch inputs were not installed")
		}
		if _, ok := checker.vars["github"]; !github.present && ok {
			t.Fatal("missing github context was introduced")
		}
		if original, ok := github.ty.Object(); ok {
			if _, ok := objectPath(t, original.Type(), "event").Property("inputs"); ok {
				t.Fatal("caller-owned github context was modified")
			}
			copied := objectPath(t, checker.vars["github"], "event", "inputs")
			if enabled, _ := copied.Property("enabled"); enabled.Kind() != KindString {
				t.Fatal("github.event.inputs did not receive string types")
			}
		}
		checker.SetContextTypes(nil)
		checker.UpdateDispatchInputs(NewStrictObjectType(map[string]Type{"again": StringType()}))
		if _, ok := checker.vars["github"]; ok {
			t.Fatal("context replacement left github available")
		}
	}
}

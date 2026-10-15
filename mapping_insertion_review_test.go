package actionlint

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestOpaqueMatrixInsertionPreservesKnownRows(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(strconv.FormatBool(nested), func(t *testing.T) {
			matrix := "os: [not-a-runner, not-a-runner]\n        exclude: [{extra: 1}]\n        ${{ insert }}: ${{ fromJSON(vars.EXTRA) }}"
			if nested {
				matrix = "${{ insert }}:\n          " + strings.ReplaceAll(matrix, "\n        ", "\n          ")
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix:\n        " + matrix + "\n    steps: [{run: echo ok}]\n"
			errs := lintCredentialsExpressionTest(t, source)
			counts := map[string]int{}
			for _, err := range errs {
				counts[err.Kind]++
			}
			if len(errs) != 3 || counts["matrix"] != 1 || counts["runner-label"] != 2 {
				t.Fatalf("known row diagnostics lost: %v", errs)
			}
			workflow, parseErrors := Parse([]byte(source))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			if _, known := knownRunnerMatrixCombinations(workflow.Jobs["test"].Strategy); known {
				t.Fatal("opaque insertion produced complete combinations")
			}
		})
	}
}

func TestDockerExpressionInsertedReservedInputs(t *testing.T) {
	for _, scope := range []string{"with", "insert", "nested"} {
		for _, tc := range []struct{ name, object, entrypoint, args, want string }{
			{"empty entrypoint", `{"entrypoint":""}`, "", "", "string should not be empty"},
			{"constant fields", `{"ENTRYPOINT":"bash","args":"hello"}`, "bash", "hello", ""},
			{"empty args", `{"entrypoint":"bash","args":""}`, "bash", "", ""},
			{"evaluated strings", `{"entrypoint":"${{ missing }}","args":"${{ missing }}"}`, "${{ missing }}", "${{ missing }}", ""},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				expression := "${{ fromJSON('" + tc.object + "') }}"
				with := fmt.Sprintf("%q", expression)
				if scope != "with" {
					with = "{'${{ insert }}': " + fmt.Sprintf("%q", expression) + "}"
				}
				if scope == "nested" {
					with = "{'${{ insert }}': " + with + "}"
				}
				source := strings.Replace(insertionSectionWorkflow("with", with), "example/action@v1", "docker://alpine:3", 1)
				errs := lintCredentialsExpressionTest(t, source)
				if tc.want != "" {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
						t.Fatalf("want %q, got %v", tc.want, errs)
					}
					return
				}
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				workflow, _ := Parse([]byte(source))
				action := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
				if action.Entrypoint == nil || action.Entrypoint.Value != tc.entrypoint || action.Args == nil || action.Args.Value != tc.args {
					t.Fatalf("reserved fields lost: %+v", action)
				}
			})
		}
	}
}

func TestConstantInsertedKeyCollisions(t *testing.T) {
	for _, section := range []string{"env", "with", "matrix"} {
		value := "one"
		if section == "matrix" {
			value = "[one]"
		}
		for _, tc := range []struct {
			name, mapping string
			duplicate     bool
		}{
			{"literal before expression", `{FOO: %s, '${{ insert }}': {"${{ 'foo' }}": %s}}`, true},
			{"expression before literal", `{'${{ insert }}': {"${{ 'foo' }}": %s}, FOO: %s}`, true},
			{"expression sibling", `{"${{ 'FOO' }}": %s, '${{ insert }}': {foo: %s}}`, true},
			{"nested expression", `{FOO: %s, '${{ insert }}': {'${{ insert }}': {"${{ 'foo' }}": %s}}}`, true},
			{"same inserted mapping", `{'${{ insert }}': {FOO: %s, "${{ 'foo' }}": %s}}`, true},
			{"numeric key", `{'42': %s, '${{ insert }}': {'${{ 42 }}': %s}}`, true},
			{"unknown key", `{FOO: %s, '${{ insert }}': {'${{ vars.KEY }}': %s}}`, false},
		} {
			t.Run(section+"/"+tc.name, func(t *testing.T) {
				_, errs := Parse([]byte(insertionSectionWorkflow(section, fmt.Sprintf(tc.mapping, value, value))))
				duplicates := 0
				for _, err := range errs {
					if strings.Contains(err.Message, "is duplicated") {
						duplicates++
					} else {
						t.Fatalf("unexpected diagnostic: %v", err)
					}
				}
				if (duplicates > 0) != tc.duplicate {
					t.Fatalf("duplicate=%v, want=%v: %v", duplicates > 0, tc.duplicate, errs)
				}
			})
		}
	}
}

func TestDockerConstantInputKeys(t *testing.T) {
	for _, scope := range []string{"with", "insert", "nested"} {
		for _, tc := range []struct{ name, mapping, entrypoint, args, want string }{
			{"empty entrypoint", `{"${{ 'entrypoint' }}": ''}`, "", "", "string should not be empty"},
			{"constant keys", `{"${{ 'ENTRYPOINT' }}": bash, "${{ 'args' }}": hello}`, "bash", "hello", ""},
			{"unknown key", `{'${{ vars.KEY }}': ''}`, "", "", ""},
			{"key expression checked", `{"${{ case(true, 'entrypoint', missing) }}": bash}`, "", "", `undefined variable "missing"`},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				mapping := tc.mapping
				for range map[string]int{"with": 0, "insert": 1, "nested": 2}[scope] {
					mapping = "{'${{ insert }}': " + mapping + "}"
				}
				source := strings.Replace(insertionSectionWorkflow("with", mapping), "example/action@v1", "docker://alpine:3", 1)
				errs := lintCredentialsExpressionTest(t, source)
				if tc.want != "" {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
						t.Fatalf("want %q, got %v", tc.want, errs)
					}
					return
				}
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				workflow, _ := Parse([]byte(source))
				action := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
				if tc.entrypoint == "" {
					if action.Entrypoint != nil || action.Args != nil {
						t.Fatal("unknown input name was resolved as a reserved field")
					}
				} else if action.Entrypoint == nil || action.Entrypoint.Value != tc.entrypoint || action.Args == nil || action.Args.Value != tc.args {
					t.Fatalf("reserved fields lost: %+v", action)
				}
			})
		}
	}
}

func TestActionInputResolutionAcrossConsumers(t *testing.T) {
	for _, key := range []string{"script", `"${{ 'script' }}"`, `"${{ 'SCRIPT' }}"`, `"${{ format('{0}', 'script') }}"`} {
		for depth := range 3 {
			t.Run(fmt.Sprintf("%s/depth=%d", key, depth), func(t *testing.T) {
				mapping := "{" + key + ": '${{ github.event.issue.title }}'}"
				for range depth {
					mapping = "{'${{ insert }}': " + mapping + "}"
				}
				source := strings.Replace(insertionSectionWorkflow("with", mapping), "example/action@v1", "actions/github-script@v8", 1)
				source = strings.Replace(source, "on: push", "on: issues", 1)
				errs := lintCredentialsExpressionTest(t, source)
				if len(errs) != 1 || !strings.Contains(errs[0].Message, "potentially untrusted") {
					t.Fatalf("script injection check missing: %v", errs)
				}
				workflow, _ := Parse([]byte(source))
				action := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
				names, complete := actionInputNames(action)
				if !complete || names["script"] == nil || len(names) != 1 {
					t.Fatalf("metadata resolved different names: %v, %v", names, complete)
				}
				if _, known := actionInputValue(action, "script"); known {
					t.Fatal("untrusted script value was treated as a constant")
				}
			})
		}
	}
}

func TestActionInputObjectResultsStayData(t *testing.T) {
	for _, mapping := range []string{
		`"${{ fromJSON('{\"script\":\"${{ github.event.issue.title }}\"}') }}"`,
		`{'${{ insert }}': "${{ fromJSON('{\"script\":\"${{ github.event.issue.title }}\"}') }}"}`,
	} {
		source := strings.Replace(insertionSectionWorkflow("with", mapping), "example/action@v1", "actions/github-script@v8", 1)
		if errs := lintCredentialsExpressionTest(t, source); len(errs) != 0 {
			t.Fatalf("evaluated script string interpreted as source: %v", errs)
		}
		workflow, _ := Parse([]byte(source))
		action := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
		if value, known := actionInputValue(action, "script"); !known || value != "${{ github.event.issue.title }}" {
			t.Fatalf("resolved value=%q, known=%v", value, known)
		}
	}
}

func TestResolvedMatrixKeysReachConsumers(t *testing.T) {
	for depth := range 3 {
		for _, key := range []string{"${{ 'os' }}", "${{ 'OS' }}", "${{ format('{0}', 'os') }}"} {
			t.Run(fmt.Sprintf("depth=%d/%s", depth, key), func(t *testing.T) {
				matrix := fmt.Sprintf("{%q: [ubuntu-latest]}", key)
				for range depth {
					matrix = "{'${{ insert }}': " + matrix + "}"
				}
				source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix: " + matrix + "\n    steps: [{run: echo ok}]\n"
				if errs := lintCredentialsExpressionTest(t, source); len(errs) != 0 {
					t.Fatalf("resolved axis rejected: %v", errs)
				}
				workflow, errs := Parse([]byte(source))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				mapping := workflow.Jobs["test"].Strategy.Matrix
				for range depth {
					mapping = mapping.Insertions[0]
				}
				row := mapping.Rows["os"]
				if row == nil || row.Name == nil || row.Name.Value != key || row.Name.Pos.Line != 6 {
					t.Fatalf("resolved ID or source key lost: %+v", row)
				}
				invalid := strings.Replace(source, "ubuntu-latest", "not-a-runner", 1)
				if errs := lintCredentialsExpressionTest(t, invalid); len(errs) != 1 || errs[0].Kind != "runner-label" {
					t.Fatalf("resolved axis bypassed runner validation: %v", errs)
				}
			})
		}
	}
}

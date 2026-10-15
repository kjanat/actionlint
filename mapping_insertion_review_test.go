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

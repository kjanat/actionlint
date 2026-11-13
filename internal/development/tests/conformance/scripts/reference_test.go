package scripts_test

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

// These expectations replay captured output. They do not execute Bash.
func TestRecordedBashErrexit(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/bash-reference.txt")
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{}
	for block := range strings.SplitSeq(strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n")), "\n\n") {
		name, output, ok := strings.Cut(block, "\n")
		if !ok || !strings.HasPrefix(name, "CASE ") {
			t.Fatalf("invalid capture %q", block)
		}
		outputs[strings.TrimPrefix(name, "CASE ")] = output
	}
	cases := []struct {
		name, script, statusMarker, exit string
		wantFinding                      bool
	}{
		{"assignment", `CHANGED="$(exit 7)"; EXIT_CODE=$?; echo CAPTURED:$EXIT_CODE`, "CAPTURED:", "7", true},
		{"guarded", `CHANGED="$(exit 7)" || EXIT_CODE=$?; echo CAPTURED:$EXIT_CODE`, "CAPTURED:7", "0", false},
		{"disabled", `set +e; false; status=$?; echo CAPTURED:$status`, "CAPTURED:1", "0", false},
		{"and", `false && echo no; status=$?; echo CAPTURED:$status`, "CAPTURED:1", "0", false},
		{"compound", `if false; then :; fi; status=$?; echo CAPTURED:$status`, "CAPTURED:0", "0", false},
		{"trap", `trap 'status=$?; echo TRAPPED:$status' ERR; false`, "TRAPPED:1", "1", false},
		{"ignored_group", `{ false; status=$?; echo CAPTURED:$status; } || :`, "CAPTURED:1", "0", false},
		{"subshell", `(set +e; false; status=$?; echo INNER:$status); false; status=$?; echo OUTER:$status`, "OUTER:", "1", true},
		{"substitution", `x=$(false; status=$?; echo INNER:$status); echo "$x"`, "INNER:1", "0", false},
	}
	if len(outputs) != len(cases) {
		t.Fatalf("capture has %d cases, expected %d", len(outputs), len(cases))
	}
	registry := interpreters.Builtin()
	invocation := registry.Analyze("bash", false)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output, ok := outputs[tc.name]
			if !ok || !strings.HasSuffix(output, "EXIT "+tc.exit) {
				t.Fatalf("unexpected recorded output %q", output)
			}
			reached := strings.Contains(output, tc.statusMarker)
			if reached == tc.wantFinding {
				t.Fatalf("recorded output %q disagrees with expected finding", output)
			}
			findings := scripts.ErrexitStatusReads(tc.script, invocation, registry)
			if (len(findings) != 0) != tc.wantFinding {
				t.Fatalf("got %+v, recorded output %q", findings, output)
			}
		})
	}
}

// Compare supported workflow-command mistakes with the runner's parser results.
func TestRecordedRunnerCommands(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/runner-reference.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flagged := map[string]bool{"::notice:Up to date": true, "::error:msg": true, "::warning single-colon": true, "::group unterminated": true, "::notic::msg": true}
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var result struct {
			Input  string `json:"input"`
			Parsed bool   `json:"parsed"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(result.Input, func(t *testing.T) {
			t.Parallel()
			var state commands.WorkflowCommandState
			problem := state.WorkflowCommandProblem(result.Input)
			if result.Parsed && problem != "" {
				t.Fatalf("runner parsed command but model reported %q", problem)
			}
			if (problem != "") != flagged[result.Input] {
				t.Fatalf("runner parsed=%v, model reported %q", result.Parsed, problem)
			}
			if problem != "" && result.Parsed {
				t.Fatal("reported command was accepted by runner")
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("got %d recorded cases", count)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestMissingPermissionsRequiredLocationsThroughCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, setting       string
		workflowPermissions bool
		jobPermissions      []bool
		wantLines           []int
	}{
		{"default workflow", "error", true, []bool{false, false}, nil},
		{"default jobs", "error", false, []bool{true, true}, nil},
		{"default uncovered job", "error", false, []bool{true, false}, []int{7}},
		{"empty options", "{level: error, options: {}}", true, []bool{false, false}, nil},
		{"require workflow", "{level: error, options: {require-at: workflow}}", false, []bool{true, true}, []int{1}},
		{"require jobs", "{level: error, options: {require-at: every-job}}", true, []bool{true, false}, []int{8}},
		{"require both missing workflow", "{level: error, options: {require-at: [workflow, every-job]}}", false, []bool{true, true}, []int{1}},
		{"require both missing job", "{level: error, options: {require-at: [workflow, every-job]}}", true, []bool{true, false}, []int{8}},
		{"require both missing both", "{level: error, options: {require-at: [workflow, every-job]}}", false, []bool{true, false}, []int{1, 7}},
		{"require both present", "{level: error, options: {require-at: [workflow, every-job]}}", true, []bool{true, true}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			contents := "tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, security: {missing-permissions: " + tc.setting + "}}}\n"
			if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var source strings.Builder
			source.WriteString("on: push\n")
			if tc.workflowPermissions {
				source.WriteString("permissions: {}\n")
			}
			source.WriteString("jobs:\n")
			for i, permissions := range tc.jobPermissions {
				source.WriteString("  " + []string{"build", "test"}[i] + ":\n")
				if permissions {
					source.WriteString("    permissions: {}\n")
				}
				source.WriteString("    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
			}
			var out, log bytes.Buffer
			status := (&Command{Stdin: strings.NewReader(source.String()), Stdout: &out, Stderr: &log}).MainContext(t.Context(), []string{"actionlint", "check", "--action-metadata=off", "--config", config, "--json", "--summary=false", "-"})
			wantStatus := 0
			if len(tc.wantLines) != 0 {
				wantStatus = 1
			}
			if status != wantStatus {
				t.Fatalf("status=%d want=%d stderr=%s stdout=%s", status, wantStatus, log.String(), out.String())
			}
			var report resultreport.CheckResult
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			var lines []int
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Rule != "missing-permissions" || diagnostic.Severity != "error" {
					t.Fatalf("unexpected diagnostic: %+v", diagnostic)
				}
				lines = append(lines, diagnostic.Start.Line)
			}
			if !slices.Equal(lines, tc.wantLines) {
				t.Fatalf("permission errors at lines %v, want %v", lines, tc.wantLines)
			}
		})
	}
}

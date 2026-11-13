package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	render "actionlint.kjanat.dev/v2/internal/output"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestEmptySummaryIsNotAllClear(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := writeCheckSummary(&out, render.ColorNever, resultreport.NewCheckSummary(resultreport.Analysis{})); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No files checked.\n" {
		t.Fatalf("empty summary: %q", out.String())
	}
}

func TestIncompleteActionCheckIsNotAllClear(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "action.yml")
	if err := os.WriteFile(path, []byte("name: test\ndescription: test\nruns: {using: node24, main: \"bad\\u0000.js\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"pretty", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			command := Command{Stdout: &out, Stderr: &log}
			args := []string{"actionlint", "check", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "--summary", "--output-format=" + format, path}
			if format == "json" {
				args = append(args, "--json")
			}
			status := command.MainContext(t.Context(), args)
			if status != 0 {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, out.String(), log.String())
			}
			if format == "json" {
				var summary struct {
					Summary resultreport.CheckSummary `json:"summary"`
				}
				if err := json.Unmarshal(log.Bytes(), &summary); err != nil {
					t.Fatal(err)
				}
				if len(summary.Summary.IncompleteChecks) != 1 || summary.Summary.IncompleteChecks[0].Rule != "action" {
					t.Fatalf("JSON summary lost incomplete checks: %s", log.String())
				}
			} else if strings.Contains(log.String(), "All clear") || !strings.Contains(log.String(), "could not finish") || !strings.Contains(log.String(), "action.yml") {
				t.Fatalf("incomplete check reported as clean: %s", log.String())
			}
		})
	}
}

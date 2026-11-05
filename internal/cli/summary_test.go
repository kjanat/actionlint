package cli

import (
	"bytes"
	"testing"

	render "actionlint.kjanat.dev/v2/internal/output"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestEmptySummaryIsNotAllClear(t *testing.T) {
	var out bytes.Buffer
	if err := writeCheckSummary(&out, render.ColorNever, resultreport.NewCheckSummary(0, nil)); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No files checked.\n" {
		t.Fatalf("empty summary: %q", out.String())
	}
}

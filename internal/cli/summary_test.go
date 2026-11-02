package cli

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/render"
)

func TestEmptySummaryIsNotAllClear(t *testing.T) {
	var out bytes.Buffer
	if err := writeCheckSummary(&out, render.ColorNever, render.NewCheckSummary(0, nil)); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No files checked.\n" {
		t.Fatalf("empty summary: %q", out.String())
	}
}

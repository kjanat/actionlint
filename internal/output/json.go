package output

import (
	"encoding/json"
	"io"

	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// WriteJSON emits findings only in JSONL mode.
func WriteJSON(out io.Writer, r report.CheckResult, lines bool) error {
	enc := json.NewEncoder(out)
	if !lines {
		return enc.Encode(r)
	}
	for _, d := range r.Diagnostics {
		if err := enc.Encode(struct {
			SchemaVersion int `json:"schema_version"`
			results.Diagnostic
		}{r.SchemaVersion, d}); err != nil {
			return err
		}
	}
	return nil
}

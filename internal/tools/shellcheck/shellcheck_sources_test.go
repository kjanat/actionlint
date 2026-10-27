package shellcheck

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/render"
)

func TestShellcheckUnavailableSourcedDiagnostic(t *testing.T) {
	rule := newShellcheckRunForTest(&externalCommand{})
	sources := make(map[string]reportedSource)
	directory := t.TempDir()
	finding := rule.sourcedDiagnostic(shellcheckError{File: "missing.sh", Line: 1, Column: 1, Code: 2086, Level: "info", Message: "finding"}, directory, sources)
	if got := render.FindingOf(finding.Diagnostic, finding.source).TemplateFields(); got.Snippet != "" || !strings.HasSuffix(got.Filepath, "missing.sh") {
		t.Fatalf("missing source borrowed workflow content: %+v", got)
	}
	for range 2 {
		if !finding.unavailableSource.Present() || finding.unavailableSource.Reason() != "read-failed" || finding.Reference != nil || len(finding.Fixes) != 0 {
			t.Fatalf("failed reread fabricated known evidence: %+v", finding)
		}
		finding = rule.sourcedDiagnostic(shellcheckError{File: "missing.sh", Line: 1, Column: 1}, directory, sources)
	}
}

package shellcheck

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/render"
)

func TestShellcheckUnavailableSourcedDiagnostic(t *testing.T) {
	rule := newShellcheckRunForTest(&externalCommand{})
	finding := rule.sourcedDiagnostic(shellcheckError{File: "missing.sh", Line: 1, Column: 1, Code: 2086, Level: "info", Message: "finding"}, t.TempDir(), make(map[string][]byte))
	if got := render.FindingOf(finding.Diagnostic, finding.source).TemplateFields(); got.Snippet != "" || !strings.HasSuffix(got.Filepath, "missing.sh") {
		t.Fatalf("missing source borrowed workflow content: %+v", got)
	}
}

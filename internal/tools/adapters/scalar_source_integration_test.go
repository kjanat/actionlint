package adapters

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/render"
)

func TestSourcedShellcheckPhysicalLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.without-extension")
	const first = "echo first\u2028still-first\u0085same-line\u2029same-line"
	const second = "echo $VALUE"
	content := []byte(first + "\n" + second + "\n")
	rule := &shellcheckRun{}
	for line, want := range []string{first, second} {
		finding := rule.sourcedDiagnostic(shellcheckError{File: path, Line: line + 1, Column: 1, Code: 2086, Message: "Quote expansion"}, "", map[string][]byte{path: content})
		if !finding.rawScriptSource {
			t.Fatal("sourced checker did not select raw script coordinates")
		}
		if got, ok := render.SourceLine(content, finding.Start.Line, finding.rawScriptSource); !ok || got != want {
			t.Fatalf("line %d = %q, %v; want %q", line+1, got, ok, want)
		}
		view := render.FindingOf(finding.Diagnostic, finding.source)
		view.RawScript = true
		fields := view.TemplateFields()
		if !strings.HasPrefix(fields.Snippet, want+"\n") {
			t.Fatalf("sourced snippet = %q; want line %q", fields.Snippet, want)
		}
	}
}

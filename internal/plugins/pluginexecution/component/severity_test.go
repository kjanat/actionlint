package component

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/results"
	"github.com/kjanat/wacogo"
)

func diagnosticWithSeverity(level string) *wacogo.ValRecord {
	base := mixedDiagnostic("m", sourcetext.Position{Line: 1, Column: 1}, sourcetext.Position{Line: 1, Column: 2})
	var fields []wacogo.Field
	for _, name := range []string{"reference", "related", "rule", "code", "severity", "message", "path", "explanation", "help", "documentation-url", "start", "end", "fixes"} {
		value := base.Field(name)
		if name == "severity" {
			value = wacogo.ValString(level)
		}
		fields = append(fields, field(name, value))
	}
	return wacogo.NewValRecord(fields...)
}

// A tool integration's manifest table is keyed by the guest's own spelling,
// so "note" must reach it unchanged.
func TestLiftDiagnosticKeepsToolSeverityForManifestTable(t *testing.T) {
	t.Parallel()
	d, err := liftDiagnostic(diagnosticWithSeverity("note"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Severity != "note" {
		t.Fatalf("lifted severity = %q, want the guest's \"note\"", d.Severity)
	}
	table := toolmanifest.Capabilities{Severities: map[string]severity.Severity{"note": severity.Error}}
	if mapped, listed := table.HostSeverity(string(d.Severity)); !listed || mapped != severity.Error {
		t.Fatalf("manifest mapping = %q, %t, want error, true", mapped, listed)
	}
	diagnostics := []results.Diagnostic{d}
	hostSeverities(diagnostics)
	if diagnostics[0].Severity != severity.Info {
		t.Fatalf("analyzer severity = %q, want info", diagnostics[0].Severity)
	}
}

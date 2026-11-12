package scriptcoords

import "testing"

func TestReportedSource(t *testing.T) {
	for name, valid := range map[string]bool{
		"lib.sh": true, "scripts/lib.sh": true, "": false, ".": false, "../lib.sh": false, "a/../../b": false,
		"/etc/passwd": false, "scripts\\lib.sh": false, "C:lib.sh": false, "a//b": false, "a\x00b": false,
	} {
		if ReportedSource(name) != valid {
			t.Errorf("ReportedSource(%q) = %t, want %t", name, !valid, valid)
		}
	}
}

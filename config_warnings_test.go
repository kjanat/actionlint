package actionlint

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigUnknownKeysWarnWithoutRejecting(t *testing.T) {
	path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yaml", "config-variable: [FOO]\nself-hosted-runner:\n  label: [custom]\npaths:\n  '**':\n    ignores: [something]\n")
	inspection, err := InspectConfig(ConfigSelection{Path: path}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Warnings) != 3 {
		t.Fatalf("warnings: %+v", inspection.Warnings)
	}
	for i, line := range []int{1, 3, 6} {
		warning := inspection.Warnings[i]
		if warning.Line != line || warning.Column < 1 || !strings.Contains(warning.Message, "ignored. Expected") {
			t.Fatalf("unhelpful warning: %+v", warning)
		}
	}
	var log bytes.Buffer
	session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, LogWriter: &log})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := session.configForProject(nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(log.String(), "warning:") != 3 {
		t.Fatalf("warnings missing or repeated: %s", &log)
	}
	if _, err := ParseConfigOverlay("config", []byte("config-variable: [FOO]")); err == nil {
		t.Fatal("new inline input must reject unknown keys")
	}
}

func TestConfigWarningSurvivesOverlay(t *testing.T) {
	path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yaml", "config-variable: [FOO]\n")
	overlay, err := ParseConfigOverlay("config", []byte("config-variables: [BAR]"))
	if err != nil {
		t.Fatal(err)
	}
	var report ConfigReport
	session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}, OnConfigLoaded: func(value ConfigReport) { report = value }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.configForProject(nil); err != nil {
		t.Fatal(err)
	}
	if len(report.Inspection.Warnings) != 1 || report.Inspection.Warnings[0].Line != 1 {
		t.Fatalf("file warning lost: %+v", report)
	}
}

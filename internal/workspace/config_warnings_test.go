package workspace_test

import (
	"bytes"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestConfigUnknownKeysWarnWithoutRejecting(t *testing.T) {
	path := writeToolFixture(t, t.TempDir(), "actionlint.yaml", `config-variable: [FOO]
self-hosted-runner:
  label: [custom]
paths:
  '**':
    ignores: [something]
`)
	inspection, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
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
	session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, LogWriter: &log})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := session.ConfigForProject(nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(log.String(), "warning:") != 3 {
		t.Fatalf("warnings missing or repeated: %s", &log)
	}
	if _, err := repoconfig.ParseOverlay("config", []byte("config-variable: [FOO]")); err == nil {
		t.Fatal("new inline input must reject unknown keys")
	}
}

func TestConfigWarningSurvivesOverlay(t *testing.T) {
	path := writeToolFixture(t, t.TempDir(), "actionlint.yaml", "config-variable: [FOO]\n")
	overlay, err := repoconfig.ParseOverlay("config", []byte("config-variables: [BAR]"))
	if err != nil {
		t.Fatal(err)
	}
	var report repoconfig.Report
	session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(value repoconfig.Report) { report = value }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ConfigForProject(nil); err != nil {
		t.Fatal(err)
	}
	if len(report.Inspection.Warnings) != 1 || report.Inspection.Warnings[0].Line != 1 {
		t.Fatalf("file warning lost: %+v", report)
	}
}

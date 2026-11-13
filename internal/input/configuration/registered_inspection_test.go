package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestInspectRegisteredRulesInInheritedConfiguration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	if err := os.WriteFile(base, []byte("lint: {rules: {disable: [no-sudo]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "actionlint.yaml")
	if err := os.WriteFile(path, []byte("extends: [base.yaml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := config.Selection{Path: path}
	inspection, err := checking.ConfigurationRules().Inspect(selection, true, "no-sudo")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Path != path || inspection.Origins["/lint/rules/disable"].File != base {
		t.Fatalf("lost inherited origins: %+v", inspection)
	}
	if _, err := checking.ConfigurationRules().Inspect(selection, false); err == nil || !strings.Contains(err.Error(), "no-sudo") {
		t.Fatalf("registered catalog leaked to later inspection: %v", err)
	}
}

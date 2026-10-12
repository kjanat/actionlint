package actionlint_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev"
)

func TestLinterRuffFieldOrder(t *testing.T) {
	typeOf := reflect.TypeFor[actionlint.LinterOptions]()
	want := []string{
		"Verbose", "Debug", "LogWriter", "Color", "Oneline", "Shellcheck",
		"Pyflakes", "IgnorePatterns", "ConfigFile", "ConfigOverlays", "OnConfigLoaded",
		"Format", "OutputFormat", "StdinFileName", "WorkingDir", "OnRulesCreated",
		"OnFilesSelected", "Context", "Ruff",
	}
	if typeOf.NumField() < len(want) {
		t.Fatalf("field count = %d, want at least %d", typeOf.NumField(), len(want))
	}
	for index, name := range want {
		if got := typeOf.Field(index).Name; got != name {
			t.Errorf("field %d = %s, want %s", index, got, name)
		}
	}
}

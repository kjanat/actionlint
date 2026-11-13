package rules

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestValueSetsDriveValidationAndDiagnostics(t *testing.T) {
	t.Parallel()
	for _, level := range Levels() {
		var decoded Level
		if err := decodeTestConfig([]byte(level), &decoded); err != nil || decoded != level || !level.Valid() {
			t.Fatalf("level %q: decoded=%q error=%v", level, decoded, err)
		}
	}
	for _, preset := range Presets() {
		var decoded Preset
		if err := decodeTestConfig([]byte(preset), &decoded); err != nil || decoded != preset || !preset.Valid() {
			t.Fatalf("preset %q: decoded=%q error=%v", preset, decoded, err)
		}
	}
	var level Level
	levelError := decodeTestConfig([]byte("unknown-level"), &level)
	if levelError == nil {
		t.Fatal("unknown level accepted")
	}
	for _, accepted := range Levels() {
		if !strings.Contains(levelError.Error(), string(accepted)) {
			t.Errorf("level diagnostic omitted %q: %v", accepted, levelError)
		}
	}
	var preset Preset
	presetError := decodeTestConfig([]byte("unknown-preset"), &preset)
	if presetError == nil {
		t.Fatal("unknown preset accepted")
	}
	for _, accepted := range Presets() {
		if !strings.Contains(presetError.Error(), string(accepted)) {
			t.Errorf("preset diagnostic omitted %q: %v", accepted, presetError)
		}
	}
}

func TestValueSetsBelongToCaller(t *testing.T) {
	t.Parallel()
	levels := Levels()
	firstLevel := levels[0]
	levels[0] = "mutated"
	if Levels()[0] != firstLevel || !firstLevel.Valid() || levels[0].Valid() {
		t.Fatal("caller changed the accepted levels")
	}
	presets := Presets()
	firstPreset := presets[0]
	presets[0] = "mutated"
	if Presets()[0] != firstPreset || !firstPreset.Valid() || presets[0].Valid() {
		t.Fatal("caller changed the accepted presets")
	}
	categories := Categories()
	firstCategory := categories[0]
	categories[0] = "mutated"
	if Categories()[0] != firstCategory || !rule.ValidCategory(firstCategory) || rule.ValidCategory(categories[0]) {
		t.Fatal("caller changed the accepted categories")
	}
}

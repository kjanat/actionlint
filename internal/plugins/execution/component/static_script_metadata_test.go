package component

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestReadScriptMetadata(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/sources/script-engine/ruff-action-input.metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadScriptMetadata(data, rule.Names{})
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := LoadComponentPlugin(t.Context(), "testdata/plugins/ruff-action-input.wasm", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer plugin.Close(context.WithoutCancel(t.Context()))
	want := plugin.ScriptDescriptor()
	if !artifacts.SameMetadata(got.Rule.OptionsSchema, want.Rule.OptionsSchema) {
		t.Fatalf("static schema = %s; runtime schema = %s", got.Rule.OptionsSchema, want.Rule.OptionsSchema)
	}
	got.Rule.OptionsSchema = want.Rule.OptionsSchema
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("static descriptor = %+v; runtime descriptor = %+v", got, want)
	}
	for _, tc := range []struct {
		name, old, replacement string
	}{
		{"other role", ScriptEngineInterface, "kjanat:actionlint/analyzer@0.1.0"},
		{"empty engine", `"name": "ruff", "version"`, `"name": "", "version"`},
		{"wrong rule identity", `"provider": "echo-component"`, `"provider": "wrong"`},
		{"unsupported template", `"metadata_version": 1,`, `"template_contract": "unknown", "metadata_version": 1,`},
		{"noncanonical shell", `"script_shells": []`, `"script_shells": ["Python.exe"]`},
		{"duplicate shells", `"script_shells": []`, `"script_shells": ["python", "python"]`},
		{"legacy fixes", `"fixes": false`, `"fixes": true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(string(data), tc.old) {
				t.Fatalf("fixture does not contain %q", tc.old)
			}
			altered := strings.Replace(string(data), tc.old, tc.replacement, 1)
			if _, err := ReadScriptMetadata([]byte(altered), rule.Names{}); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

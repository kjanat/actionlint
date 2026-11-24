package component_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestAdmitRunsEveryStaticLoadCheck(t *testing.T) {
	t.Parallel()
	read := func(path ...string) []byte {
		data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, path...)...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{
			"empty invocation definition",
			fixtureregistry.ReplaceMetadata(read("plugins", "no-sudo.wasm"), `{"metadata_version":1,"id":"no-sudo","interface":"`+component.AnalyzerInterface+`","invocation_definitions":[{"id":""}]}`),
			`duplicate or empty interpreter definition ""`,
		},
		{
			"template contract without template effects",
			fixtureregistry.ReplaceMetadata(read("plugins", "script-engine-0.3.0.wasm"), `{"interface":"`+component.ScriptEngine03Interface+`","template_contract":"`+scriptengine.TemplateEffectsContract+`"}`),
			"static template contract disagrees with exported contract",
		},
		{
			"script metadata version",
			fixtureregistry.ReplaceMetadata(read("plugins", "script-engine-0.3.0.wasm"), `{"metadata_version":2,"interface":"`+component.ScriptEngine03Interface+`"}`),
			"script engine static metadata disagrees with its typed descriptor",
		},
		{
			"tail call",
			read("wat", "tail-call.wasm"),
			`return_call invalid as feature "tail-call" is disabled`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, checked := component.Admit(t.Context(), component.ComponentBytes(tc.data))
			if checked == nil || !strings.Contains(checked.Error(), tc.want) {
				t.Fatalf("admission error=%v, want %q", checked, tc.want)
			}
			path := filepath.Join(t.TempDir(), "plugin.wasm")
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			plugin, loaded := component.LoadComponentPlugin(t.Context(), path, component.Options{})
			if loaded == nil {
				_ = plugin.Close(t.Context())
			}
			if want := "plugin " + path + ": " + checked.Error(); loaded == nil || loaded.Error() != want {
				t.Fatalf("load error=%v, want %q", loaded, want)
			}
		})
	}
}

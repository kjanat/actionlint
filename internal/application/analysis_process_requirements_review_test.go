package application_test

import (
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"testing"
)

func TestRequiredToolsIncludesProcessBackendOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, overrides string
		want            bool
	}{
		{name: "all wasm"},
		{name: "composite process", overrides: "[{includes: ['python-action/**'], tools: {ruff: {backend: {process: {executable: ruff}}}}}]", want: true},
		{name: "process then catch-all wasm", overrides: "[{includes: ['python-action/**'], tools: {ruff: {backend: {process: {executable: ruff}}}}}, {includes: ['**'], tools: {ruff: {backend: {wasm: {path: final.wasm}}}}}]"},
		{name: "catch-all wasm then process", overrides: "[{includes: ['**'], tools: {ruff: {backend: {wasm: {path: final.wasm}}}}}, {includes: ['python-action/**'], tools: {ruff: {backend: {process: {executable: ruff}}}}}]", want: true},
		{name: "disabled process", overrides: "[{includes: ['python-action/**'], tools: {ruff: {enabled: false, backend: {process: {executable: ruff}}}}}]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, ".git", "")
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/python-action}]\n")
			writeFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps: [{shell: python, run: print(missing)}]\n")
			config := "tools: {ruff: {backend: {wasm: {path: engine.wasm}}}}\n"
			if tc.overrides != "" {
				config += "overrides: " + tc.overrides + "\n"
			}
			writeFixture(t, root, ".github/actionlint.yaml", config)
			for _, command := range []string{"ruff", ""} {
				options := application.Options{WorkingDir: root, Tools: (adapters.Options{Ruff: command}).Registrations()}
				needed, err := application.RequiredTools(t.Context(), options, []string{workflow})
				if err != nil || needed["ruff"] != (tc.want && command != "") {
					t.Fatalf("command %q: needed=%+v, error=%v", command, needed, err)
				}
			}
		})
	}
}

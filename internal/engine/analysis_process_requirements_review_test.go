package engine

import "testing"

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
			writeShellcheckFixture(t, root, ".git", "")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/python-action}]\n")
			writeShellcheckFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps: [{shell: python, run: print(missing)}]\n")
			config := "tools: {ruff: {backend: {wasm: {path: engine.wasm}}}}\n"
			if tc.overrides != "" {
				config += "overrides: " + tc.overrides + "\n"
			}
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
			for _, command := range []string{"ruff", ""} {
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Ruff: command})
				if err != nil {
					t.Fatal(err)
				}
				needed, err := session.RequiredTools([]string{workflow})
				if err != nil || needed.Ruff != (tc.want && command != "") {
					t.Fatalf("command %q: needed=%+v, error=%v", command, needed, err)
				}
			}
		})
	}
}

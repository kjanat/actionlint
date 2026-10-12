package actionlint

import "testing"

func TestMetadataCacheWithReusableIgnores(t *testing.T) {
	for _, tc := range []struct {
		name, ignores string
		want          int
	}{
		{"none", "[]", 1},
		{"expression", "[{rule: expression, job: first}]", 1},
		{"shellcheck", "[{rule: shellcheck, job: first}]", 1},
		{"missing-local-action", "[{rule: missing-local-action, job: first}]", 1},
		{"action-file-matched", "[{rule: action, file: '.github/workflows/ci.yml'}]", 0},
		{"action-first-job", "[{rule: action, job: first}]", 1},
		{"action-second-job", "[{rule: action, job: second}]", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nignores: "+tc.ignores+"\n")
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
			source := "on: push\njobs:\n  first:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n  second:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
			path := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", source)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{path}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("want %d diagnostics, got %+v", tc.want, result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if d.Rule != "action" {
					t.Fatalf("unexpected diagnostic: %+v", d)
				}
				wantLine := 6
				if tc.name == "action-first-job" {
					wantLine = 10
				}
				if d.Start.Line != wantLine {
					t.Fatalf("expected finding at invocation line %d, got %+v", wantLine, d)
				}
			}
		})
	}
}

package application_test

import (
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

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
			writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nignores: "+tc.ignores+"\n")
			writeFixture(t, root, "inner/action.yml", "name: inner\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
			source := "on: push\njobs:\n  first:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n  second:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
			path := writeFixture(t, root, ".github/workflows/ci.yml", source)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
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
				wantLine := map[string]int{"action-first-job": 10, "action-second-job": 6}[tc.name]
				if wantLine != 0 && d.Start.Line != wantLine {
					t.Fatalf("expected finding at invocation line %d, got %+v", wantLine, d)
				}
			}
		})
	}
}

package application_test

import (
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

func TestExecutableBitNumericFormatCheckout(t *testing.T) {
	root, _ := executableFixture(t)
	for _, tc := range []struct {
		argument string
		want     int
	}{
		{"0", 1},
		{"42", 1},
		{"-42", 1},
		{"-0", 0},
		{"1e15", 0},
		{"0.12345678901234567", 0},
	} {
		t.Run(tc.argument, func(t *testing.T) {
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        if: ${{ format('{0}', ` + tc.argument + `) }}
      - run: ./bad.sh
`
			file := writeFixture(t, root, ".github/workflows/format.yml", workflow)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{file}})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "executable-bit" {
					count++
				}
			}
			if count != tc.want {
				t.Fatalf("got %d executable-bit findings, want %d: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}

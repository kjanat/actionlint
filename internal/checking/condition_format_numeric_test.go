package checking_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestExecutableBitNumericFormatCheckout(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			root, _ := executableFixture(t)
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        if: ${{ format('{0}', ` + tc.argument + `) }}
      - run: ./bad.sh
`
			file := writeFixture(t, root, workflowFixturePath(t, "format"), workflow)
			session := checking.Options{WorkingDir: root}
			var err error

			result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{file}})
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

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeprecatedPyflakesFlag(t *testing.T) {
	workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(undefined_name)
`
	for _, prefix := range [][]string{nil, {"check"}} {
		args := append(append([]string{}, prefix...), "--pyflakes=does-not-exist --invalid", "--json", "-")
		got := testRunCommand(workflow, args...)
		if got.Status != 0 || !strings.Contains(got.Stderr, "deprecated and ignored") {
			t.Fatalf("%v: %+v", args, got)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(got.Stderr), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatalf("non-JSON warning: %s", line)
			}
		}
	}
}

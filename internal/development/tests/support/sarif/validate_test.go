package sarif

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	const valid = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"test"}},"results":[{"level":"error","message":{"text":"example"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"workflow.yml"},"region":{"startLine":1}}}]}]}]}`
	if err := Validate([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ before, after string }{
		{`"level":"error"`, `"level":"fatal"`},
		{`"startLine":1`, `"startLine":0`},
		{`"uri":"workflow.yml"`, `"uri":"bad path.yml"`},
	} {
		t.Run(change.after, func(t *testing.T) {
			t.Parallel()
			if err := Validate([]byte(strings.Replace(valid, change.before, change.after, 1))); err == nil {
				t.Fatalf("OASIS schema accepted %s", change.after)
			}
		})
	}
}

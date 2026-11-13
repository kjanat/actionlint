package typecheck

import (
	"os"
	"strconv"
	"testing"

	reference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"
)

func TestCollectionIdentityReference(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_REFERENCE_IMAGE") == "" {
		t.Skip("set ACTIONLINT_REFERENCE_IMAGE to compare against Runny")
	}
	variables := map[string]any{"env": map[string]any{"OBJECT": `{"allowed":true}`, "ARRAY": `[1,2]`, "NUMBER": `42`}, "github": map[string]any{"event": map[string]any{"number": 1}}}
	variables["matrix"] = map[string]any{"a.b": map[string]any{}, "a": map[string]any{"b": map[string]any{}}}
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{`fromJSON(env.OBJECT) == fromJSON(env.OBJECT)`, false},
		{`fromJSON(env.OBJECT) != fromJSON(env.OBJECT)`, true},
		{`fromJSON(env.ARRAY) == fromJSON(env.ARRAY)`, false},
		{`fromJSON(env.ARRAY) != fromJSON(env.ARRAY)`, true},
		{`fromJSON(env.NUMBER) == fromJSON(env.NUMBER)`, true},
		{`github.event == github.event`, true},
		{`matrix['a.b'] == matrix.a.b`, false},
		{`matrix['a.b'] == MATRIX['A.B']`, true},
		{`fromJSON(env.OBJECT) != null`, true},
		{`fromJSON(env.OBJECT).allowed == fromJSON(env.OBJECT).allowed`, true},
	} {
		for _, mode := range []reference.JSONMode{reference.StrictJSON, reference.LooseJSON} {
			t.Run(tc.expression+"/strict="+strconv.FormatBool(mode == reference.StrictJSON), func(t *testing.T) {
				t.Parallel()
				reference.CheckOutcomeWithJSONMode(t, tc.expression, variables, mode, reference.Expectation{Value: tc.want})
			})
		}
	}
}

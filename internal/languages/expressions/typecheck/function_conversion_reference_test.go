package typecheck

import (
	"os"
	"strconv"
	"testing"

	reference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"
)

func TestFunctionConversionReference(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_REFERENCE_IMAGE") == "" {
		t.Skip("set ACTIONLINT_REFERENCE_IMAGE to run the pinned expression reference")
	}
	variables := map[string]any{
		"steps":  map[string]any{"check": map[string]any{"outputs": map[string]any{"available": "false"}}},
		"matrix": map[string]any{"collection": []any{"value", true}},
	}
	for _, tc := range []struct {
		expression string
		want       reference.Expectation
	}{
		{"fromJSON(steps.check.outputs).available", reference.Expectation{Failure: reference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"fromJSON(matrix.collection)", reference.Expectation{Failure: reference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"fromJSON(steps.check.outputs.available)", reference.Expectation{Value: false}},
		{"fromJSON(true)", reference.Expectation{Value: true}},
		{"fromJSON(42)", reference.Expectation{Value: float64(42)}},
		{"join('[github.server_url,github.repository]', '/')", reference.Expectation{Value: "[github.server_url,github.repository]"}},
		{"join('ordinary text', '/')", reference.Expectation{Value: "ordinary text"}},
	} {
		for _, mode := range []reference.JSONMode{reference.StrictJSON, reference.LooseJSON} {
			t.Run(tc.expression+"/strict="+strconv.FormatBool(mode == reference.StrictJSON), func(t *testing.T) {
				t.Parallel()
				want := tc.want
				if want.Failure == reference.RuntimeFailure && mode == reference.LooseJSON {
					want.ExceptionType = "Newtonsoft.Json.JsonReaderException"
				}
				reference.CheckOutcomeWithJSONMode(t, tc.expression, variables, mode, want)
			})
		}
	}
}

func TestOutputStringTruthinessReference(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_REFERENCE_IMAGE") == "" {
		t.Skip("set ACTIONLINT_REFERENCE_IMAGE to run the pinned expression reference")
	}
	// Use expression evaluation to test boolean conversion. Runny's eval-if
	// reader can treat an unwrapped condition as literal text.
	for _, output := range []string{"", "false", "true"} {
		for _, mode := range []reference.JSONMode{reference.StrictJSON, reference.LooseJSON} {
			t.Run(output+"/strict="+strconv.FormatBool(mode == reference.StrictJSON), func(t *testing.T) {
				t.Parallel()
				variables := map[string]any{"steps": map[string]any{"check": map[string]any{"outputs": map[string]any{"available": output}}}}
				reference.CheckOutcomeWithJSONMode(t, "!!steps.check.outputs.available", variables, mode, reference.Expectation{Value: output != ""})
			})
		}
	}
}

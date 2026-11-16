package inputs

import (
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func selectedGraphInput(tb testing.TB, count int) AnalyzerInput {
	tb.Helper()
	scripts := make([]invocationfixture.Script, count)
	for index := range scripts {
		scripts[index] = invocationfixture.Script{Node: strconv.Itoa(index), Entry: true}
	}
	graph := invocationfixture.Graph(tb, scripts...)
	selected := graph.Scripts()[0]
	return AnalyzerInput{
		Reference: graph.Entry().Reference, AvailableInputs: []rule.InputKind{rule.Invocations, rule.Scripts}, CallGraph: graph,
		SelectedScripts: []invocation.ID{selected.ID}, Scripts: []Script{{Reference: selected.Declaration, Occurrence: selected}},
	}
}

// This bounds allocations only; timing remains benchmark evidence.
func TestSelectedInvocationValidationAllocationBound(t *testing.T) {
	for _, count := range []int{1, 256, 10000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			input := selectedGraphInput(t, count)
			var validationErr error
			allocations := testing.AllocsPerRun(10, func() { validationErr = input.ValidateInvocations() })
			if validationErr != nil {
				t.Fatal(validationErr)
			}
			if allocations > 2 {
				t.Fatalf("single-script validation allocated %.0f times for %d graph records; limit 2", allocations, count)
			}
		})
	}
}

func BenchmarkSelectedInvocationValidation(b *testing.B) {
	for _, count := range []int{1, 256, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			input := selectedGraphInput(b, count)
			b.ReportAllocs()
			for b.Loop() {
				if err := input.ValidateInvocations(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

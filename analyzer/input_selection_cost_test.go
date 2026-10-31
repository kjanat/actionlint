package analyzer

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func selectedGraphInput(t testing.TB, count int) Input {
	t.Helper()
	entry := InvocationEntry{ID: OccurrenceID(strings.Repeat("a", 64)), Kind: WorkflowEntry, Reference: source.Reference{Document: "workflow", Node: "document"}}
	records := CallGraphData{Entry: entry, Budget: InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: count}, Complete: true}
	for index := range count {
		records.Scripts = append(records.Scripts, ScriptOccurrence{
			ID: OccurrenceID(fmt.Sprintf("%064x", index+1)), Owner: entry.ID, Ancestry: []OccurrenceID{entry.ID},
			Declaration: source.Reference{Document: "workflow", Node: strconv.Itoa(index)}, Kind: RunScript,
		})
	}
	graph, err := NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	selected := records.Scripts[0]
	return Input{
		Reference: entry.Reference, Available: []Fact{Invocations, Scripts}, CallGraph: graph,
		SelectedScripts: []OccurrenceID{selected.ID}, Scripts: []Script{{Reference: selected.Declaration, Occurrence: selected}},
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

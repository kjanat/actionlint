package analyzer

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func TestCallGraphPagesRetainOrderAndIsolateReaders(t *testing.T) {
	records := graphFixture(t)
	callOutcome := records.Calls[0].Outcome
	records.Calls = nil
	records.Scripts = nil
	records.Budget.MaxScripts = 2*MaxCallGraphPageSize + 1
	records.Budget.MaxCalls = 2*MaxCallGraphPageSize + 1
	for index := range records.Budget.MaxCalls {
		records.Calls = append(records.Calls, Call{
			ID: OccurrenceID(fmt.Sprintf("%064x", index+1)), Parent: records.Entry.ID,
			Depth: 1, Ancestry: []OccurrenceID{records.Entry.ID}, Site: source.Reference{Document: "workflow", Revision: "r", Node: strconv.Itoa(index)},
			Condition: CallEnabled, Outcome: callOutcome,
		})
	}
	for index := range records.Budget.MaxScripts {
		records.Scripts = append(records.Scripts, ScriptOccurrence{
			ID: OccurrenceID(fmt.Sprintf("%064x", index+10000)), Owner: records.Entry.ID,
			Ancestry: []OccurrenceID{records.Entry.ID}, Kind: RunScript,
			Declaration: source.Reference{Document: "workflow", Revision: "r", Node: strconv.Itoa(index)},
		})
	}
	graph := retainGraph(t, records)
	for reader := range 4 {
		t.Run(strconv.Itoa(reader), func(t *testing.T) {
			t.Parallel()
			var collectedCalls []Call
			for offset := 0; offset < graph.CallCount(); offset += MaxCallGraphPageSize {
				page, err := graph.CallsPage(offset, MaxCallGraphPageSize)
				if err != nil {
					t.Fatal(err)
				}
				collectedCalls = append(collectedCalls, page...)
			}
			if !reflect.DeepEqual(collectedCalls, records.Calls) {
				t.Fatal("paged call traversal omitted, reordered or changed calls")
			}
			for index := range collectedCalls {
				collectedCalls[index].Ancestry[0] = "changed"
				call, found := graph.Call(collectedCalls[index].ID)
				if !found || call.Ancestry[0] != records.Entry.ID {
					t.Fatal("reader mutation changed call ancestry")
				}
			}
			var collected []ScriptOccurrence
			for offset := 0; offset < graph.ScriptCount(); offset += MaxCallGraphPageSize {
				page, err := graph.ScriptsPage(offset, MaxCallGraphPageSize)
				if err != nil {
					t.Fatal(err)
				}
				collected = append(collected, page...)
			}
			if !reflect.DeepEqual(collected, records.Scripts) {
				t.Fatal("paged traversal omitted, reordered or changed occurrences")
			}
			for index := range collected {
				collected[index].Ancestry[0] = "changed"
				script, found := graph.Script(collected[index].ID)
				if !found || script.Ancestry[0] != records.Entry.ID {
					t.Fatal("reader mutation changed ancestry")
				}
			}
		})
	}
}

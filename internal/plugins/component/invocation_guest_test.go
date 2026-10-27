package component

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"github.com/kjanat/wacogo"
)

func TestGuestReadsPagedGraphWithSelectedText(t *testing.T) {
	graph, calls, scripts := invocationContextPagedGraph(t, 257)
	var logs []string
	guest, err := Load(t.Context(), "testdata/plugins/misbehave.wasm", Options{Log: func(line string) { logs = append(logs, line) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guest.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	occurrence, _ := graph.Script(scripts[0])
	input := analyzer.Input{
		Reference: graph.Entry().Reference, Available: []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Invocations},
		CallGraph: graph, SelectedScripts: []analyzer.OccurrenceID{occurrence.ID}, Options: json.RawMessage("invocation-graph"),
		Invoked: []analyzer.Script{{Reference: occurrence.Declaration, Occurrence: occurrence, Text: "echo chosen", Start: diagnostic.Position{Line: 1, Column: 1}}},
	}
	if findings, err := guest.Analyze(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("guest graph traversal: findings=%v error=%v", findings, err)
	}
	want := make([]string, 0, len(calls)+len(scripts)+1)
	for _, id := range calls {
		call, _ := graph.Call(id)
		want = append(want, fmt.Sprintf("call:%s:%s", id, call.Parent))
	}
	for _, id := range scripts {
		want = append(want, fmt.Sprintf("script:%s:%s", id, occurrence.Owner))
	}
	want = append(want, fmt.Sprintf("graph:%s:257:257:1", graph.Entry().ID))
	if !slices.Equal(logs, want) {
		t.Fatalf("guest lost graph records: received %d log lines, want %d", len(logs), len(want))
	}
	logs = nil
	input.SelectedScripts, input.Invoked = []analyzer.OccurrenceID{}, nil
	if _, err := guest.Analyze(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if logs[len(logs)-1] != fmt.Sprintf("graph:%s:257:257:0", graph.Entry().ID) {
		t.Fatal("some(empty) selection changed at guest boundary")
	}
	logs = nil
	if _, err := guest.Analyze(t.Context(), analyzer.Input{Options: input.Options}); err == nil || !strings.Contains(err.Error(), "invocation graph is unavailable") {
		t.Fatalf("graph leaked into following call: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("following call saw prior graph: %v", logs)
	}
}

func TestLowerInputPreservesSelectionAndActionInput(t *testing.T) {
	for _, selected := range [][]analyzer.OccurrenceID{nil, {}, {"chosen"}} {
		value := lowerInput(analyzer.Input{SelectedScripts: selected})
		option, ok := value.Field("selected-scripts").(*wacogo.ValOption)
		if !ok || option.IsNone() != (selected == nil) {
			t.Fatalf("selection changed: %v", selected)
		}
		if selected != nil {
			list, err := liftList(option.Val(), "selected scripts")
			if err != nil || list.Len() != len(selected) {
				t.Fatalf("selection values changed: %v", err)
			}
		}
	}
	input := analyzer.ActionInput{Occurrence: analyzer.ScriptOccurrence{ID: "chosen"}, Site: analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}}
	value := lowerInput(analyzer.Input{ActionInputs: []analyzer.ActionInput{input}})
	list, err := liftList(value.Field("action-inputs"), "action inputs")
	if err != nil || list.Len() != 1 {
		t.Fatalf("action input omitted: %v", err)
	}
	record, err := liftRecord(list.Get(0), "action input")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"action": input.Site.Action, "input": input.Site.Input, "language": input.Site.Language} {
		if got, err := liftString(record, name); err != nil || got != want {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
}

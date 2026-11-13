package component

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"github.com/kjanat/wacogo"
)

func TestGuestReadsPagedGraphWithSelectedText(t *testing.T) {
	t.Parallel()
	graph, calls, scripts := invocationContextPagedGraph(t, 257)
	var logs []string
	// 257 pages take about 1s natively; race and coverage builds run 10x slower
	// than the default limit allows. The limit is not under test here.
	guest, err := Load(t.Context(), "testdata/plugins/misbehave.wasm", Options{Timeout: time.Minute, Log: func(line string) { logs = append(logs, line) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guest.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	occurrence, _ := graph.Script(scripts[0])
	input := checkinputs.Input{
		Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Source, rulemeta.Scripts, rulemeta.Invocations},
		CallGraph: graph, SelectedScripts: []checkinputs.OccurrenceID{occurrence.ID}, Options: json.RawMessage("invocation-graph"),
		Invoked: []checkinputs.Script{{Reference: occurrence.Declaration, Text: "echo chosen", Start: sourcepos.Position{Line: 1, Column: 1}, Occurrence: occurrence}},
	}
	if findings, err := guest.CheckDocument(t.Context(), input); err != nil || len(findings) != 0 {
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
	input.SelectedScripts, input.Invoked = []checkinputs.OccurrenceID{}, nil
	if _, err := guest.CheckDocument(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if logs[len(logs)-1] != fmt.Sprintf("graph:%s:257:257:0", graph.Entry().ID) {
		t.Fatal("some(empty) selection changed at guest boundary")
	}
	logs = nil
	if _, err := guest.CheckDocument(t.Context(), checkinputs.Input{Options: input.Options}); err == nil || !strings.Contains(err.Error(), "invocation graph is unavailable") {
		t.Fatalf("graph leaked into following call: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("following call saw prior graph: %v", logs)
	}
}

func TestLowerInputPreservesSelectionAndActionInput(t *testing.T) {
	t.Parallel()
	for _, selected := range [][]checkinputs.OccurrenceID{nil, {}, {"chosen"}} {
		value := lowerInput(checkinputs.Input{SelectedScripts: selected})
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
	input := checkinputs.ActionInput{Occurrence: checkinputs.ScriptOccurrence{ID: "chosen"}, Site: checkinputs.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}}
	value := lowerInput(checkinputs.Input{ActionInputs: []checkinputs.ActionInput{input}})
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

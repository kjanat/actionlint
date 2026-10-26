package invocation_test

import (
	"encoding/json"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestIdentityVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/identity-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Cases []struct {
			Name  string
			Entry struct {
				Kind      invocation.EntryKind
				Reference source.Reference
			}
			Calls []struct {
				Key, Parent string
				Reference   source.Reference
			}
			Expected struct {
				RootID invocation.ID `json:"root_id"`
				Calls  map[string]struct {
					ID    invocation.ID
					Depth int
				}
			}
		}
		Scripts []struct {
			Owner       invocation.ID
			Declaration source.Reference
			Kind        invocation.ScriptKind
			Input       string
			Expected    invocation.ID
		} `json:"script_cases"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Cases) != 6 || len(vectors.Scripts) != 3 {
		t.Fatalf("missing vectors: %d roots, %d scripts", len(vectors.Cases), len(vectors.Scripts))
	}
	for _, vector := range vectors.Cases {
		t.Run(vector.Name, func(t *testing.T) {
			root, err := invocation.NewRootID(vector.Entry.Kind, vector.Entry.Reference)
			if err != nil || root != vector.Expected.RootID {
				t.Fatalf("root %q, want %q: %v", root, vector.Expected.RootID, err)
			}
			ids := map[string]invocation.ID{"root": root}
			for _, call := range vector.Calls {
				parent, exists := ids[call.Parent]
				if !exists {
					t.Fatalf("fixture parent %q is missing", call.Parent)
				}
				id, err := invocation.NewCallID(parent, call.Reference)
				if err != nil || id != vector.Expected.Calls[call.Key].ID {
					t.Fatalf("call %s: %q, want %q: %v", call.Key, id, vector.Expected.Calls[call.Key].ID, err)
				}
				ids[call.Key] = id
			}
		})
	}
	for _, script := range vectors.Scripts {
		id, err := invocation.NewScriptID(script.Owner, script.Declaration, script.Kind, script.Input)
		if err != nil || id != script.Expected {
			t.Errorf("script %s/%s: %q, want %q: %v", script.Kind, script.Input, id, script.Expected, err)
		}
	}
}

func TestEntryKindSeparatesOccurrences(t *testing.T) {
	ref := reference("same-source", "document")
	workflow, err := invocation.NewRootID(invocation.EntryWorkflow, ref)
	if err != nil {
		t.Fatal(err)
	}
	action, err := invocation.NewRootID(invocation.EntryAction, ref)
	if err != nil || action == workflow {
		t.Fatalf("entry kinds share occurrence identity: %v", err)
	}
}

func TestInvalidIdentityInputs(t *testing.T) {
	good := reference("entry", "document")
	for name, ref := range map[string]source.Reference{
		"missing-document": {Node: "node"},
		"missing-node":     {Document: "doc"},
		"invalid-document": {Document: "\xff", Node: "node"},
		"invalid-revision": {Document: "doc", Node: "node", Revision: "\xff"},
		"invalid-address":  {Document: "doc", Node: "node", Address: "\xff"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := invocation.NewRootID(invocation.EntryWorkflow, ref); err == nil {
				t.Fatal("accepted invalid source identity")
			}
		})
	}
	if _, err := invocation.NewRootID("unknown", good); err == nil {
		t.Fatal("accepted unknown entry kind")
	}
	for _, id := range []invocation.ID{"", "unknown", "\xff"} {
		if _, err := invocation.NewCallID(id, good); err == nil {
			t.Errorf("accepted malformed parent %q", id)
		}
		if _, err := invocation.NewScriptID(id, good, invocation.ScriptRun, ""); err == nil {
			t.Errorf("accepted malformed script owner %q", id)
		}
	}
}

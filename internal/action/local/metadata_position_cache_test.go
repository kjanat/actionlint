package local

import (
	"encoding/json"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestMetadataPositionCacheConstruction(t *testing.T) {
	value := &actionmeta.ExprString{Value: "example", Line: 2, Column: 3}
	checkSourceLess := func(meta *actionmeta.Metadata) {
		t.Helper()
		if meta.ScalarSource(value) != nil {
			t.Fatal("source-less metadata has a position cache")
		}
		for range 2 {
			if got := actionmeta.ExpressionPosition(meta, value, 2); *got != (workflowlang.Pos{Line: 2, Col: 3}) || meta.ScalarSource(value) != nil {
				t.Fatal("source-less metadata did not retain its fallback position")
			}
		}
	}
	for spec, meta := range actioncatalog.PopularActions {
		if meta == nil {
			t.Fatalf("generated action %s has no metadata", spec)
		}
		checkSourceLess(meta)
	}
	var decoded struct {
		Metadata *actionmeta.Metadata `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"test","inputs":{}}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata == nil {
		t.Fatal("JSONL-shaped metadata was not decoded")
	}
	checkSourceLess(decoded.Metadata)
	if err := json.Unmarshal([]byte(`{"inputs":{"token":{"name":"token"}}}`), decoded.Metadata); err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata.Name != "test" || decoded.Metadata.Inputs["token"] == nil {
		t.Fatal("JSON update lost existing fields or did not decode new fields")
	}
	checkSourceLess(decoded.Metadata)
	root := t.TempDir()
	writeFixture(t, root, "source/action.yml", "name: source\ndescription: source\nruns: {using: composite, steps: []}\n")
	base := NewCache(root, nil)
	meta, _, err := base.FindMetadata("./source")
	if err != nil || meta == nil || meta.Root == nil {
		t.Fatalf("local metadata cache: %+v, %v", meta, err)
	}
	view := NewResolver(base, map[string]string{"./generated": "./source"})
	mapped, _, err := view.FindMetadata("./generated")
	if err != nil || mapped == nil || mapped.Root != meta.Root {
		t.Fatalf("mapped metadata lost shared cache: %+v, %v", mapped, err)
	}
	writeFixture(t, root, "empty/action.yml", "null\n")
	empty, _, err := base.FindMetadata("./empty")
	if err != nil || empty == nil || empty.ScalarSource(value) != nil {
		t.Fatalf("null YAML bypassed cache initialization: %+v, %v", empty, err)
	}

	checkSourceLess(&actionmeta.Metadata{})
}

func TestMappedMetadataRetainsLookupState(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "source/action.yml", "name: source\ndescription: source\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	base := NewCache(root, nil)
	resolver := NewResolver(base, map[string]string{"./generated": "./source"})
	var first *actionmeta.Metadata
	for _, spec := range []string{"./source", "./generated", "./source", "./generated"} {
		meta, _, err := resolver.FindMetadata(spec)
		if err != nil || meta == nil {
			t.Fatalf("lookup %q: metadata=%+v error=%v", spec, meta, err)
		}
		if meta.SourceMapped != (spec == "./generated") {
			t.Fatalf("lookup %q inherited another lookup's mapping state", spec)
		}
		if first == nil {
			first = meta
			continue
		}
		if meta.Root != first.Root || meta.Path() != first.Path() || string(meta.Source) != string(first.Source) {
			t.Fatalf("lookup %q lost its shared declaration snapshot", spec)
		}
	}
	if first.SourceMapped {
		t.Fatal("mapped lookup mutated cached declaration metadata")
	}
}

package engine

import (
	"encoding/json"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/actioncatalog"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workspace"
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
	writeShellcheckFixture(t, root, "source/action.yml", "name: source\ndescription: source\nruns: {using: composite, steps: []}\n")
	base := actionlocal.NewCache(projectRoot(workspace.Identity(root)), nil)
	meta, _, err := base.FindMetadata("./source")
	if err != nil || meta == nil || meta.Root == nil {
		t.Fatalf("local metadata cache: %+v, %v", meta, err)
	}
	view := actionlocal.NewResolver(base, map[string]string{"./generated": "./source"})
	mapped, _, err := view.FindMetadata("./generated")
	if err != nil || mapped == nil || mapped.Root != meta.Root {
		t.Fatalf("mapped metadata lost shared cache: %+v, %v", mapped, err)
	}
	writeShellcheckFixture(t, root, "empty/action.yml", "null\n")
	empty, _, err := base.FindMetadata("./empty")
	if err != nil || empty == nil || empty.ScalarSource(value) != nil {
		t.Fatalf("null YAML bypassed cache initialization: %+v, %v", empty, err)
	}
	for _, content := range []string{"null\n", "name: standalone\nruns: {using: composite, steps: []}\n"} {
		outline, err := ParseActionOutline("action.yml", []byte(content))
		if (err != nil) != (content == "null\n") || outline.Path != "action.yml" {
			t.Fatalf("standalone construction: %+v, %v", outline, err)
		}
	}
	checkSourceLess(&actionmeta.Metadata{})
}

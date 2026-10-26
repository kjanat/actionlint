package codec

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
	"go.yaml.in/yaml/v4/plugin/limit"
)

type cycleAccountingTarget struct {
	Items map[string]any `yaml:"items"`
}

type cycleAccountingPlugin struct {
	defaults               *limit.Plugin
	aliases, constructions int
}

func newCycleAccountingPlugin() *cycleAccountingPlugin {
	return &cycleAccountingPlugin{defaults: limit.New()}
}

func (p *cycleAccountingPlugin) CheckDepth(depth int, ctx *upstream.DepthContext) error {
	return p.defaults.CheckDepth(depth, ctx)
}

func (p *cycleAccountingPlugin) CheckAlias(aliases, constructions int) error {
	p.aliases, p.constructions = aliases, constructions
	return p.defaults.CheckAlias(aliases, constructions)
}

func TestCursorCycleAccountingMatchesUpstream(t *testing.T) {
	const source = "a: &a {c: *a}\nitems: *a\n"
	var root syntax.Node
	if err := Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("parse source graph: %v", err)
	}
	graph, err := toParser(&root)
	if err != nil {
		t.Fatalf("convert graph for upstream baseline: %v", err)
	}
	baselineCounts := newCycleAccountingPlugin()
	var baseline cycleAccountingTarget
	baselineErr := graph.root.Load(&baseline, upstream.WithV3Defaults(), upstream.WithPlugin(baselineCounts))
	if baselineErr == nil || !strings.Contains(baselineErr.Error(), "contains itself") {
		t.Fatalf("upstream recursive alias error = %v, want cycle error", baselineErr)
	}

	session, rootCursor, err := Open(&root)
	if err != nil {
		t.Fatalf("open cursor session: %v", err)
	}
	defer session.Close()
	rootValue, err := rootCursor.Enter()
	if err != nil {
		t.Fatalf("enter root: %v", err)
	}
	var selected Entered
	for key, value := range rootValue.Pairs() {
		name, err := key.Enter()
		if err != nil {
			t.Fatalf("enter mapping key: %v", err)
		}
		if name.Node().Value == "a" {
			continue
		}
		if name.Node().Value != "items" {
			continue
		}
		items, err := value.Enter()
		if err != nil {
			t.Fatalf("enter items alias: %v", err)
		}
		for childKey, childValue := range items.Pairs() {
			childName, err := childKey.Enter()
			if err != nil {
				t.Fatalf("enter recursive key: %v", err)
			}
			if childName.Node().Value != "c" {
				continue
			}
			selected, err = childValue.Enter()
			if err != nil {
				t.Fatalf("enter recursive alias occurrence: %v", err)
			}
			break
		}
		break
	}
	if selected.Node() == nil {
		t.Fatal("did not enter recursive alias value")
	}
	var cursorTarget map[string]any
	cursorErr := selected.Decode(&cursorTarget)
	if cursorErr == nil || !strings.Contains(cursorErr.Error(), "contains itself") {
		t.Fatalf("cursor recursive alias error = %v, want cycle error", cursorErr)
	}
	if got, want := [2]int{session.aliases, session.constructions}, [2]int{baselineCounts.aliases, baselineCounts.constructions}; got != want {
		t.Errorf("cursor construction counts = aliases:%d constructions:%d, upstream = aliases:%d constructions:%d", got[0], got[1], want[0], want[1])
	}
	t.Logf("cursor counters aliases:%d constructions:%d; upstream aliases:%d constructions:%d", session.aliases, session.constructions, baselineCounts.aliases, baselineCounts.constructions)
}

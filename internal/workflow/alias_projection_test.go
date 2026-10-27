package workflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestYAMLAliasProjectionIsRepeatableAndOwnsItsNodes(t *testing.T) {
	root, err := codec.Parse([]byte("base: &base {nested: {value: original}}\none: *base\ntwo: *base\n"))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotAliasTestGraph(root)
	report := func(_ *syntax.Node, _ YamlAliasDiagnostic, message string) { t.Fatal(message) }
	first, firstOrigins := projectAliasTestGraph(root, report)
	second, secondOrigins := projectAliasTestGraph(root, report)
	assertAliasTestProjection(t, first, before)
	assertAliasTestProjection(t, second, before)
	assertAliasTestProjection(t, second, snapshotAliasTestGraph(first))
	if !reflect.DeepEqual(first, second) || len(firstOrigins) != 2 || len(secondOrigins) != 2 {
		t.Fatal("repeated projection changed graph or alias provenance")
	}
	for _, index := range []int{3, 5} {
		original := root.Content[0].Content[index]
		if firstOrigins[first.Content[0].Content[index]] != original || secondOrigins[second.Content[0].Content[index]] != original {
			t.Fatal("projection lost original alias-use identity")
		}
	}
	first.Content[0].Content[3].Content[1].Content[1].Value = "modified"
	first.Content[0].HeadComment = "projection only"
	assertAliasTestProjection(t, first, before)
	if second.Content[0].Content[3].Content[1].Content[1].Value != "original" {
		t.Fatal("projection mutation crossed into a separate parse")
	}
}

func TestYAMLAliasProjectionKeepsSharedExpansionBounded(t *testing.T) {
	var source strings.Builder
	source.WriteString("a0: &a0 [value]\n")
	const depth = 8
	for level := 1; level <= depth; level++ {
		fmt.Fprintf(&source, "a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
	}
	fmt.Fprintf(&source, "result: *a%d\n", depth)
	root, err := codec.Parse([]byte(source.String()))
	if err != nil {
		t.Fatal(err)
	}
	originals := snapshotAliasTestGraph(root)
	projected, origins := projectAliasTestGraph(root, func(_ *syntax.Node, _ YamlAliasDiagnostic, message string) { t.Fatal(message) })
	assertAliasTestProjection(t, projected, originals)
	if len(origins) != 3*depth+1 {
		t.Fatalf("projected alias uses=%d, want %d", len(origins), 3*depth+1)
	}
	if count, bound := len(snapshotAliasTestGraph(projected)), len(originals)+len(origins); count > bound {
		t.Fatalf("shared declarations expanded into %d nodes, bound %d", count, bound)
	}
}

func TestWorkflowParserProjectsAfterPhysicalSourceIndexing(t *testing.T) {
	const source = "on: push\nenv:\n  CONDITION: &condition github.ref == 'refs/heads/main'\njobs:\n  check:\n    if: *condition\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotAliasTestGraph(root)
	anchor := root.Content[0].Content[3].Content[1]
	workflow, findings := parseDocument([]byte(source), root)
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	assertAliasTestSourceUnchanged(t, before)
	condition := workflow.Jobs["check"].If
	if condition == nil || condition.Value != anchor.Value {
		t.Fatalf("aliased condition changed: %+v", condition)
	}
	mapped, exists := workflow.ExpressionSources[*PosAt(anchor)]
	if !exists || mapped.Value != anchor.Value || len(workflow.ExpressionSources) != 1 {
		t.Fatalf("projected condition lost its anchored expression source: %+v", workflow.ExpressionSources)
	}
}

func TestWorkflowEmptyParseDoesNotChangePhysicalSource(t *testing.T) {
	root, err := codec.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	workflow, findings := parseDocument(nil, root)
	if !reflect.ValueOf(*root).IsZero() || workflow == nil || len(findings) != 1 || findings[0].Message != "workflow is empty" || findings[0].Line != 1 || findings[0].Column != 1 {
		t.Fatalf("empty document: source=%+v workflow=%+v findings=%+v", root, workflow, findings)
	}
}

func TestWorkflowParseEmptyInputs(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte(" \n \n"), []byte("# comment\n")} {
		workflow, findings := Parse(data)
		if workflow == nil || len(findings) != 1 || findings[0].Kind != "syntax-check" || findings[0].Message != "workflow is empty" || findings[0].Line != 1 || findings[0].Column != 1 {
			t.Fatalf("empty source %q: workflow=%+v findings=%+v", data, workflow, findings)
		}
	}
}

func TestWorkflowDocumentDriverHasIndependentParserState(t *testing.T) {
	const source = "on: push\nenv: &bad+name {A: value}\njobs:\n  check:\n    runs-on: ubuntu-latest\n    env: *bad+name\n    steps: [{run: echo ok}]\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotAliasTestGraph(root)
	first, firstErrors := parseDocument([]byte(source), root)
	second, secondErrors := parseDocument([]byte(source), root)
	assertAliasTestSourceUnchanged(t, before)
	if first == nil || second == nil || len(firstErrors) != 2 || !reflect.DeepEqual(firstErrors, secondErrors) || len(first.Jobs) != 1 || len(second.Jobs) != 1 {
		t.Fatalf("driver reused parser state: first=%+v second=%+v", firstErrors, secondErrors)
	}
}

func TestYAMLAliasGraphPreconditionRejectsNilProgrammerInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		root *syntax.Node
	}{
		{"nil root", nil},
		{"nil child", &syntax.Node{Kind: syntax.DocumentNode, Content: []*syntax.Node{nil}}},
	} {
		for _, project := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/project=%t", tc.name, project), func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Fatal("malformed internal source graph did not fail loudly")
					}
				}()
				report := func(*syntax.Node, YamlAliasDiagnostic, string) {}
				if project {
					projectAliasTestGraph(tc.root, report)
				} else {
					ValidateYAMLAliases(tc.root, report)
				}
			})
		}
	}
}

func TestWorkflowParsePreservesAliasTypeErrorProvenance(t *testing.T) {
	const source = "on: push\nenv:\n  FLAG: &flag {}\njobs:\n  check:\n    runs-on: ubuntu-latest\n    continue-on-error: *flag\n    steps: [{run: echo ok}]\n"
	workflow, findings := Parse([]byte(source))
	if workflow == nil {
		t.Fatal(findings)
	}
	aliases := 0
	for _, finding := range findings {
		if strings.Contains(finding.Message, `alias "flag"`) {
			aliases++
			if finding.Line != 7 || finding.Column != 24 || finding.Kind != "syntax-check" || !strings.Contains(finding.Message, "expected bool value") || !strings.Contains(finding.Message, `alias "flag" refers to the anchor at line:3, column:9`) {
				t.Fatalf("alias type diagnostic lost provenance: %+v", finding)
			}
		}
	}
	if aliases != 1 {
		t.Fatalf("alias diagnostics=%d, findings=%+v", aliases, findings)
	}
}

func TestWorkflowParseRejectsRecursiveMatrixAlias(t *testing.T) {
	const source = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value: &cycle [*cycle]\n    steps: [{run: echo ok}]\n"
	workflow, findings := Parse([]byte(source))
	if workflow == nil {
		t.Fatal(findings)
	}
	recursive := 0
	for _, finding := range findings {
		if strings.Contains(finding.Message, `recursive alias "cycle"`) {
			recursive++
			if finding.Line != 7 || finding.Column != strings.Index(strings.Split(source, "\n")[6], "*cycle")+1 || finding.Kind != "syntax-check" {
				t.Fatalf("recursive alias location lost: %+v", finding)
			}
		}
	}
	if recursive != 1 {
		t.Fatalf("recursive reports=%d findings=%+v", recursive, findings)
	}
}

func TestWorkflowParseReportsAnchoredFlowNullOnce(t *testing.T) {
	const source = "on: push\nenv: &env {MISSING:}\njobs:\n  first:\n    runs-on: ubuntu-latest\n    env: *env\n    steps: [{run: echo ok}]\n  second:\n    runs-on: ubuntu-latest\n    env: *env\n    steps: [{run: echo ok}]\n"
	workflow, findings := Parse([]byte(source))
	if workflow == nil || len(workflow.Jobs) != 2 {
		t.Fatal(findings)
	}
	if len(findings) != 1 || findings[0].Line != 2 || findings[0].Column != 12 || !strings.Contains(findings[0].Message, "space after ':'") {
		t.Fatalf("flow-null should be reported once on physical anchor: %+v", findings)
	}
}

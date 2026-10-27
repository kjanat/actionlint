package workflow

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func aliasAdmissionWorkflow(depth int) []byte {
	var source strings.Builder
	source.WriteString("on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        a0: &a0 [value]\n")
	for level := 1; level <= depth; level++ {
		fmt.Fprintf(&source, "        a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
	}
	source.WriteString("    steps: [{run: echo ok}]\n")
	return []byte(source.String())
}

func TestWorkflowAdmissionRejectsAmplificationBeforeASTConstruction(t *testing.T) {
	for _, depth := range []int{8, 12, 24} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			source := aliasAdmissionWorkflow(depth)
			root, err := codec.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			before := snapshotAliasTestGraph(root)
			workflow, findings := parseDocument(source, root)
			if workflow != nil || len(findings) != 2 || findings[0].Message != fmt.Sprintf("anchor %q is defined but not used", fmt.Sprintf("a%d", depth)) {
				t.Fatalf("amplified AST recovered or physical findings lost: workflow=%v findings=%v", workflow, findings)
			}
			failure := findings[1]
			if failure.Message != "could not parse as YAML: document contains excessive aliasing" || failure.Kind != "syntax-check" {
				t.Fatal(failure)
			}
			physical := false
			for node := range before {
				physical = physical || failure.Line == node.Line && failure.Column == node.Column
			}
			if !physical {
				t.Fatalf("admission failure lost its physical source: %+v", failure)
			}
			assertAliasTestSourceUnchanged(t, before)
			second, repeated := Parse(source)
			if second != nil || !reflect.DeepEqual(findings, repeated) {
				t.Fatal("admission changes across repeated parses")
			}
		})
	}
}

func TestWorkflowAdmissionPreservesRecursiveDiagnostics(t *testing.T) {
	source := append([]byte("env: {cycle: &cycle [*cycle]}\n"), aliasAdmissionWorkflow(8)...)
	workflow, findings := Parse(source)
	if workflow != nil {
		t.Fatal("over-budget document supplied semantic input")
	}
	var recursive, budget int
	for _, finding := range findings {
		if strings.Contains(finding.Message, "recursive alias") {
			recursive++
		}
		if strings.Contains(finding.Message, "excessive aliasing") {
			budget++
		}
	}
	if recursive != 1 || budget != 1 {
		t.Fatalf("recovery diagnostic or admission error lost: %v", findings)
	}
}

func TestWorkflowAdmissionDoesNotGrowWithAliasExpansion(t *testing.T) {
	small, large := aliasAdmissionWorkflow(12), aliasAdmissionWorkflow(24)
	allocations := func(source []byte) float64 {
		return testing.AllocsPerRun(2, func() {
			workflow, findings := Parse(source)
			if workflow != nil || len(findings) != 2 {
				t.Fatal("fixture must be rejected before AST construction")
			}
		})
	}
	smallCost, largeCost := allocations(small), allocations(large)
	if largeCost > 2*smallCost {
		t.Fatalf("doubling physical depth multiplied admission allocations: small=%v large=%v", smallCost, largeCost)
	}
}

func TestWorkflowAdmissionCoversSkippedValuesAndParallelSteps(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(strconv.FormatBool(parallel), func(t *testing.T) {
			var source strings.Builder
			source.WriteString("on: push\nenv:\n  a0: &a0 [{run: echo ok}]\n")
			for level := 1; level <= 8; level++ {
				if parallel {
					fmt.Fprintf(&source, "  a%d: &a%d [{parallel: *a%d}, {parallel: *a%d}, {parallel: *a%d}]\n", level, level, level-1, level-1, level-1)
				} else {
					fmt.Fprintf(&source, "  a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
				}
			}
			if !parallel {
				source.WriteString("  ? [invalid, key]\n  : *a8\n")
			}
			source.WriteString("jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: *a8\n")
			workflow, findings := Parse([]byte(source.String()))
			if workflow != nil || len(findings) == 0 || !strings.Contains(findings[len(findings)-1].Message, "excessive aliasing") {
				t.Fatalf("non-matrix expansion bypassed admission: workflow=%v findings=%v", workflow, findings)
			}
		})
	}
}

func TestWorkflowAdmissionAllowsNonAmplifyingReuse(t *testing.T) {
	var source strings.Builder
	source.WriteString("on: push\nenv: &shared {VALUE: literal}\njobs:\n")
	for job := range 100 {
		fmt.Fprintf(&source, "  build%d:\n    runs-on: ubuntu-latest\n    env: *shared\n    steps: [{run: echo ok}]\n", job)
	}
	workflow, findings := Parse([]byte(source.String()))
	if workflow == nil || len(workflow.Jobs) != 100 || len(findings) != 0 {
		t.Fatalf("ordinary reuse rejected: findings=%v", findings)
	}
}

func TestWorkflowAdmissionSkipsRecursiveSlotsInsideAcceptedAnchors(t *testing.T) {
	const source = "on: push\nenv:\n  base: &base {self: *base}\n  copy: *base\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	workflow, findings := Parse([]byte(source))
	if workflow == nil {
		t.Fatal("recursive recovery became an admission failure", findings)
	}
	var recursive int
	for _, finding := range findings {
		if strings.Contains(finding.Message, "recursive alias") {
			recursive++
		}
		if strings.Contains(finding.Message, "contains itself") || strings.Contains(finding.Message, "excessive aliasing") {
			t.Fatal("rejected recursive slot was followed", finding)
		}
	}
	if recursive != 1 {
		t.Fatalf("recursive use changed diagnostics: %v", findings)
	}
}

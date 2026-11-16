package checking_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results"
)

type invocationCollector struct {
	mu      sync.Mutex
	scripts []invocationObservation
}

type invocationObservation struct {
	checkinputs.Script
	graph invocation.Graph
}

func (*invocationCollector) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "invocation-identity", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution}}
}

func (c *invocationCollector) CheckDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	for _, script := range input.Invoked {
		c.scripts = append(c.scripts, invocationObservation{Script: script, graph: input.CallGraph})
	}
	c.mu.Unlock()
	return nil, nil
}

func TestStandaloneActionInvocationDepth(t *testing.T) {
	t.Parallel()
	root, _ := executableFixture(t)
	outer := writeFixture(t, root, "outer/action.yml", "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - uses: $/nested\n")
	writeFixture(t, root, "nested/action.yml", "name: nested\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo nested\n")
	collector := &invocationCollector{}
	_, err := checking.Check(t.Context(), checking.Options{
		WorkingDir: root,
		Analyzers:  []checkinputs.Registration{{Checker: collector}},
	}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{outer}})
	if err != nil {
		t.Fatal(err)
	}
	collector.mu.Lock()
	scripts := append([]invocationObservation(nil), collector.scripts...)
	collector.mu.Unlock()
	if len(scripts) != 1 {
		t.Fatalf("nested script invocation missing: %+v", scripts)
	}
	script := scripts[0]
	call, found := script.graph.Call(script.Occurrence.Owner)
	if !found || call.Depth() != 1 || call.Parent() != script.graph.RootID() || call.Uses() != "$/nested" {
		t.Fatalf("standalone nested call lost its root: %+v", call)
	}
	if err := checkinputs.ValidateOccurrence(script.graph, script.Occurrence); err != nil {
		t.Fatal(err)
	}
}

func TestNestedInvocationIdentity(t *testing.T) {
	t.Parallel()
	const oneCall = `on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: $/outer
`
	for _, test := range []struct {
		name      string
		workflows []string
		middle    bool
	}{
		{name: "same job", workflows: []string{oneCall + "      - uses: $/outer\n"}},
		{name: "different jobs", workflows: []string{oneCall + "  other:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/outer\n"}},
		{name: "third level", workflows: []string{oneCall + "      - uses: $/outer\n"}, middle: true},
		{name: "different workflows", workflows: []string{oneCall, oneCall}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, _ := executableFixture(t)
			outerTarget := "nested"
			caller := "outer/action.yml"
			depth := 2
			if test.middle {
				outerTarget, caller, depth = "middle", "middle/action.yml", 3
				writeFixture(t, root, caller, "name: middle\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - uses: $/nested\n")
			}
			writeFixture(t, root, "outer/action.yml", "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - uses: $/"+outerTarget+"\n")
			writeFixture(t, root, "nested/action.yml", "name: nested\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo nested\n")
			var paths []string
			for i, content := range test.workflows {
				name := "ci.yml"
				if i > 0 {
					name = "other.yml"
				}
				paths = append(paths, writeFixture(t, root, name, content))
			}
			collector := &invocationCollector{}
			_, err := checking.Check(t.Context(), checking.Options{
				WorkingDir: root,
				Analyzers:  []checkinputs.Registration{{Checker: collector}},
			}, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
			if err != nil {
				t.Fatal(err)
			}
			collector.mu.Lock()
			scripts := append([]invocationObservation(nil), collector.scripts...)
			collector.mu.Unlock()
			if len(scripts) != 2 {
				t.Fatalf("invoked scripts = %d, want 2", len(scripts))
			}
			for _, script := range scripts {
				if script.Text != "echo nested" || !strings.HasSuffix(filepath.ToSlash(script.Path), "nested/action.yml") || script.Reference.Document == "" {
					t.Fatalf("missing script declaration: %+v", script)
				}
				call, found := script.graph.Call(script.Occurrence.Owner)
				if !found || call.Site().Document == "" || !strings.HasSuffix(filepath.ToSlash(call.Site().Document), caller) || call.Uses() != "$/nested" || call.Depth() != depth {
					t.Fatalf("missing immediate call site: %+v", call)
				}
			}
			if scripts[0].Reference != scripts[1].Reference {
				t.Fatal("one declaration acquired different source identities")
			}
			left, right := scripts[0], scripts[1]
			leftCall, _ := left.graph.Call(left.Occurrence.Owner)
			rightCall, _ := right.graph.Call(right.Occurrence.Owner)
			if leftCall.Site() != rightCall.Site() {
				t.Fatal("one immediate call site acquired different source identities")
			}
			ancestry := map[invocation.ID][]invocation.ID{}
			for _, script := range scripts {
				occurrence := script.Occurrence
				if err := checkinputs.ValidateOccurrence(script.graph, occurrence); err != nil {
					t.Fatal(err)
				}
				chain, _ := script.graph.Chain(occurrence.Owner)
				if len(chain) != depth+1 || chain[0] != script.graph.RootID() || chain[depth] != occurrence.Owner {
					t.Fatalf("incomplete ancestry: %+v", chain)
				}
				for level, id := range chain[1:] {
					call, found := script.graph.Call(id)
					if !found || call.Depth() != level+1 || call.Parent() != chain[level] {
						t.Fatalf("broken ancestry link at level %d: %+v", level, call)
					}
				}
				ancestry[occurrence.ID] = chain
			}
			leftChain, rightChain := ancestry[left.Occurrence.ID], ancestry[right.Occurrence.ID]
			if left.Occurrence.ID == right.Occurrence.ID || left.Occurrence.Owner == right.Occurrence.Owner || leftChain[1] == rightChain[1] {
				t.Fatal("distinct outer calls collapsed into one occurrence")
			}
			leftOuter, _ := left.graph.Call(leftChain[1])
			rightOuter, _ := right.graph.Call(rightChain[1])
			if leftOuter.Site() == rightOuter.Site() {
				t.Fatal("distinct workflow call sites lost their source identity")
			}
		})
	}
}

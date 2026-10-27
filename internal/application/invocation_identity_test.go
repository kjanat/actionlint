package application_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

type invocationCollector struct {
	mu      sync.Mutex
	scripts []invocationObservation
}

type invocationObservation struct {
	analyzer.Script
	graph analyzer.CallGraph
}

func (*invocationCollector) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "invocation-identity", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution}}
}

func (c *invocationCollector) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
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
	root, _ := executableFixture(t)
	outer := writeFixture(t, root, "outer/action.yml", "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - uses: $/nested\n")
	writeFixture(t, root, "nested/action.yml", "name: nested\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo nested\n")
	collector := &invocationCollector{}
	_, err := application.Check(t.Context(), application.Options{
		WorkingDir: root,
		Analyzers:  []analyzer.Registration{{Analyzer: collector}},
	}, application.Input{Kind: application.Files, Paths: []string{outer}})
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
	if !found || call.Depth != 1 || call.Parent != script.graph.Entry().ID || call.Uses != "$/nested" {
		t.Fatalf("standalone nested call lost its root: %+v", call)
	}
	if err := script.graph.ValidateOccurrence(script.Occurrence); err != nil {
		t.Fatal(err)
	}
}

func TestNestedInvocationIdentity(t *testing.T) {
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
			_, err := application.Check(t.Context(), application.Options{
				WorkingDir: root,
				Analyzers:  []analyzer.Registration{{Analyzer: collector}},
			}, application.Input{Kind: application.Files, Paths: paths})
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
				invocation, found := script.graph.Call(script.Occurrence.Owner)
				if !found || invocation.Site.Document == "" || !strings.HasSuffix(filepath.ToSlash(invocation.Site.Document), caller) || invocation.Uses != "$/nested" || invocation.Depth != depth {
					t.Fatalf("missing immediate call site: %+v", invocation)
				}
			}
			if scripts[0].Reference != scripts[1].Reference {
				t.Fatal("one declaration acquired different source identities")
			}
			left, right := scripts[0], scripts[1]
			leftCall, _ := left.graph.Call(left.Occurrence.Owner)
			rightCall, _ := right.graph.Call(right.Occurrence.Owner)
			if leftCall.Site != rightCall.Site {
				t.Fatal("one immediate call site acquired different source identities")
			}
			for _, script := range scripts {
				occurrence := script.Occurrence
				if err := script.graph.ValidateOccurrence(occurrence); err != nil {
					t.Fatal(err)
				}
				if occurrence.Depth != depth || len(occurrence.Ancestry) != depth+1 || occurrence.Ancestry[0] != script.graph.Entry().ID || occurrence.Ancestry[depth] != occurrence.Owner {
					t.Fatalf("incomplete ancestry: %+v", occurrence)
				}
				for level, id := range occurrence.Ancestry[1:] {
					call, found := script.graph.Call(id)
					if !found || call.Depth != level+1 || call.Parent != occurrence.Ancestry[level] {
						t.Fatalf("broken ancestry link at level %d: %+v", level, call)
					}
				}
			}
			if left.Occurrence.ID == right.Occurrence.ID || left.Occurrence.Owner == right.Occurrence.Owner || left.Occurrence.Ancestry[1] == right.Occurrence.Ancestry[1] {
				t.Fatal("distinct outer calls collapsed into one occurrence")
			}
			leftOuter, _ := left.graph.Call(left.Occurrence.Ancestry[1])
			rightOuter, _ := right.graph.Call(right.Occurrence.Ancestry[1])
			if leftOuter.Site == rightOuter.Site {
				t.Fatal("distinct workflow call sites lost their source identity")
			}
		})
	}
}

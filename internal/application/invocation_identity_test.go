package application_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	scripts []analyzer.Script
}

func (*invocationCollector) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "invocation-identity", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution}}
}

func (c *invocationCollector) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	c.mu.Lock()
	c.scripts = append(c.scripts, input.Invoked...)
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
	scripts := append([]analyzer.Script(nil), collector.scripts...)
	collector.mu.Unlock()
	if len(scripts) != 1 || scripts[0].Invocation == nil {
		t.Fatalf("nested script invocation missing: %+v", scripts)
	}
	if invocation := scripts[0].Invocation; invocation.Depth != 1 {
		t.Fatalf("standalone root has no incoming call: nested depth = %d, want 1", invocation.Depth)
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
			scripts := append([]analyzer.Script(nil), collector.scripts...)
			collector.mu.Unlock()
			if len(scripts) != 2 {
				t.Fatalf("invoked scripts = %d, want 2", len(scripts))
			}
			for _, script := range scripts {
				if script.Text != "echo nested" || !strings.HasSuffix(filepath.ToSlash(script.Path), "nested/action.yml") || script.Reference.Document == "" {
					t.Fatalf("missing script declaration: %+v", script)
				}
				invocation := script.Invocation
				if invocation == nil || invocation.Reference.Document == "" || !strings.HasSuffix(filepath.ToSlash(invocation.Caller), caller) || invocation.Uses != "$/nested" || invocation.Depth != depth {
					t.Fatalf("missing immediate call site: %+v", invocation)
				}
			}
			if scripts[0].Reference != scripts[1].Reference {
				t.Fatal("one declaration acquired different source identities")
			}
			if scripts[0].Invocation.Reference != scripts[1].Invocation.Reference {
				t.Fatal("one immediate call site acquired different source identities")
			}
			first, err := json.Marshal(scripts[0].Invocation)
			if err != nil {
				t.Fatal(err)
			}
			second, err := json.Marshal(scripts[1].Invocation)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(first, second) {
				t.Fatalf("distinct outer calls have identical portable invocation facts: %s", first)
			}
		})
	}
}

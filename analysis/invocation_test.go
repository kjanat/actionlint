package analysis_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type invocationAnalyzer struct {
	mu     sync.Mutex
	inputs []analyzer.Input
}

func (*invocationAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "composite-scripts", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution}}
}

func (a *invocationAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.inputs = append(a.inputs, input)
	a.mu.Unlock()
	var findings []diagnostic.Diagnostic
	for _, script := range input.Invoked {
		start, ok := script.Position(1, 6, false)
		if !ok {
			continue
		}
		end, _ := script.Position(1, 11, true)
		call, found := input.CallGraph.Call(script.Occurrence.Owner)
		if !found {
			return nil, fmt.Errorf("missing owner %q", script.Occurrence.Owner)
		}
		findings = append(findings, diagnostic.Diagnostic{Reference: &script.Reference, Message: "invoked " + call.Uses, Path: script.Path, Start: start, End: end})
	}
	return findings, nil
}

func TestPublicCompositeInvocationFacts(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		".git/HEAD":                        "ref: refs/heads/main\n",
		".github/actions/greet/action.yml": "name: greet\ndescription: greet\nruns:\n  using: composite\n  steps:\n    - run: echo hello\n      shell: bash\n",
		".github/workflows/ci.yml":         "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/greet\n      - run: echo hi\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &invocationAnalyzer{}
	workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
	result, err := analysis.Files(t.Context(), []string{workflow}, analysis.FileOptions{
		WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: a}}, SkipProjectConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.inputs) != 1 {
		t.Fatalf("inputs=%d", len(a.inputs))
	}
	input := a.inputs[0]
	if len(input.Scripts) != 1 || len(input.Invoked) != 1 {
		t.Fatalf("scripts=%+v invoked=%+v", input.Scripts, input.Invoked)
	}
	declared := input.Scripts[0]
	if declared.Path != "" || declared.JobID != "build" || declared.Occurrence.Owner != input.CallGraph.Entry().ID || declared.Runtime == nil {
		t.Fatalf("declared script=%+v", declared)
	}
	if declared.Runtime.Shell != "bash" || !declared.Runtime.ShellKnown || !declared.Runtime.ShellImplicit {
		t.Fatalf("declared runtime=%+v", declared.Runtime)
	}
	invoked := input.Invoked[0]
	if !strings.HasSuffix(filepath.ToSlash(invoked.Path), ".github/actions/greet/action.yml") || invoked.JobID != "build" || invoked.Text != "echo hello" || invoked.Shell != "bash" {
		t.Fatalf("invoked script=%+v", invoked)
	}
	call, found := input.CallGraph.Call(invoked.Occurrence.Owner)
	if !found || call.Uses != "./.github/actions/greet" || call.Parent != input.CallGraph.Entry().ID || call.Step.Line != 6 || call.Depth != 1 {
		t.Fatalf("invocation=%+v", call)
	}
	if invoked.Reference.Node == "" || invoked.Reference.Document == input.Reference.Document || call.Site.Document != input.Reference.Document || call.Site.Node == "" || invoked.Reference.Address != "runs.steps[0]" {
		t.Fatalf("source and invocation identities were conflated: %+v %+v", invoked.Reference, call.Site)
	}
	if invoked.Runtime == nil || invoked.Runtime.Shell != "bash" || invoked.Runtime.ShellImplicit || !invoked.Runtime.DirectoryKnown || invoked.Runtime.Directory != resolved {
		t.Fatalf("invoked runtime=%+v (root %s)", invoked.Runtime, resolved)
	}
	if invoked.Start != (diagnostic.Position{Line: 6, Column: 12}) {
		t.Fatalf("invoked start=%+v", invoked.Start)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Reference == nil || *finding.Reference != invoked.Reference || finding.Rule != "composite-scripts" || !strings.HasSuffix(filepath.ToSlash(finding.Path), ".github/actions/greet/action.yml") || finding.Start != (diagnostic.Position{Line: 6, Column: 17}) || finding.End != (diagnostic.Position{Line: 6, Column: 22}) {
		t.Fatalf("finding=%+v", finding)
	}
}

func TestNestedCompositeCallerHasIdentityWithoutDeclaredScripts(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		".git/HEAD":                "ref: refs/heads/main\n",
		".github/workflows/ci.yml": "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./outer\n",
		"outer/action.yml":         "name: outer\ndescription: outer\nruns:\n  using: composite\n  steps:\n    - id: call-inner\n      uses: ./inner\n",
		"inner/action.yml":         "name: inner\ndescription: inner\nruns:\n  using: composite\n  steps:\n    - run: echo hello\n      shell: bash\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	capture := &invocationAnalyzer{}
	_, err := analysis.Files(t.Context(), []string{filepath.Join(root, ".github", "workflows", "ci.yml")}, analysis.FileOptions{WorkingDir: root, SkipProjectConfig: true, Analyzers: []analyzer.Registration{{Analyzer: capture}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.inputs) != 1 || len(capture.inputs[0].Invoked) != 1 {
		t.Fatalf("inputs: %+v", capture.inputs)
	}
	input := capture.inputs[0]
	script := input.Invoked[0]
	call, found := input.CallGraph.Call(script.Occurrence.Owner)
	if !found || call.Site.Node == "" || !strings.HasSuffix(call.Site.Document, "/outer/action.yml") || call.Site.Address != "runs.steps[0]" || !strings.HasSuffix(script.Reference.Document, "/inner/action.yml") {
		t.Fatalf("lost intermediate caller identity: %+v %+v", script.Reference, call)
	}
}

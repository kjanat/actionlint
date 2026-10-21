package localapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
)

type actionInputObserver struct{ input analyzer.Input }

func (*actionInputObserver) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "action-input-observer", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution}}
}

func (o *actionInputObserver) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	o.input = input
	return nil, nil
}

func TestPackagedComponentAnalyzesActionInputs(t *testing.T) {
	root := t.TempDir()
	workflow := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@v7\n        with:\n          script: |\n            console.log(\"${{ 'missing' }}\");\n            missing();\n      - uses: ./nested\n      - uses: example/execute@v1\n        with:\n          code: \"console.log('é'); missing();\"\n      - uses: actions/upload-artifact@v4\n        with:\n          name: missing\n      - run: echo missing\n        shell: bash\n"
	composite := "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v7\n      with:\n        script: >\n          console.log('é');\n          missing();\n"
	for name, content := range map[string]string{".git/HEAD": "ref: refs/heads/master\n", "workflow.yml": workflow, "nested/action.yml": composite} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	module, err := filepath.Abs("../plugins/component/testdata/plugins/action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	observer := &actionInputObserver{}
	result, err := Check(t.Context(), application.Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: observer}}}, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}, []string{module})
	if err != nil {
		t.Fatal(err)
	}
	want := map[diagnostic.Position]string{
		{Line: 10, Column: 13}: "workflow.yml",
		{Line: 14, Column: 36}: "workflow.yml",
		{Line: 10, Column: 11}: "action.yml",
	}
	count := 0
	for _, finding := range result.Diagnostics {
		if finding.Rule != "echo" {
			continue
		}
		count++
		path, ok := want[finding.Start]
		if !ok || filepath.Base(finding.Path) != path || finding.End.Line != finding.Start.Line || finding.End.Column != finding.Start.Column+7 {
			t.Fatalf("action input lost declaring coordinates: %+v", finding)
		}
		delete(want, finding.Start)
	}
	if count != 3 || len(want) != 0 {
		t.Fatalf("input findings=%d remaining=%+v diagnostics=%+v", count, want, result.Diagnostics)
	}
	if len(observer.input.ActionInputs) != 3 || len(observer.input.Scripts) != 1 || len(observer.input.Invoked) != 0 {
		t.Fatalf("action input facts leaked into run facts: %+v", observer.input)
	}
	invoked := 0
	for _, input := range observer.input.ActionInputs {
		if input.Site.Language != "javascript" || input.Shell != "" || input.Runtime != nil || len(input.Spans) == 0 {
			t.Fatalf("action input invented shell facts or lost mapping: %+v", input)
		}
		if input.Invocation != nil {
			invoked++
			if input.Invocation.Uses != "./nested" || !strings.HasSuffix(input.Path, "action.yml") {
				t.Fatalf("input lost composite invocation: %+v", input)
			}
		}
	}
	if invoked != 1 {
		t.Fatalf("composite input invocations=%d", invoked)
	}
}

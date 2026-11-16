package execution

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func TestCheckoutEnvironmentPlatform(t *testing.T) {
	t.Parallel()
	for _, platform := range []runners.Platform{runners.Windows, runners.MacOrLinux, runners.Any} {
		for _, name := range []string{"git_work_tree", "path", "Node_Options", "ssh_askpass", "git_allow_protocol"} {
			t.Run(fmt.Sprintf("%s/%d", name, platform), func(t *testing.T) {
				t.Parallel()
				env := &definition.Env{Vars: map[string]*definition.EnvVar{name: {Name: &definition.String{Value: name}, Value: &definition.String{Value: "override"}}}}
				semantic := mapping.Resolve(definition.EnvMappingView(env))
				if got, want := CheckoutEnvironmentUnknown(semantic, platform), platform != runners.MacOrLinux; got != want {
					t.Fatalf("checkout uncertainty = %v, want %v", got, want)
				}
				expression := &definition.Env{Insertions: []*definition.Env{{Expression: &definition.String{Value: fmt.Sprintf("${{ fromJSON('{%q:\"override\"}') }}", name)}}}}
				semantic = mapping.Resolve(definition.EnvMappingView(expression))
				if got, want := CheckoutEnvironmentUnknown(semantic, platform), platform != runners.MacOrLinux; got != want {
					t.Fatalf("inserted checkout uncertainty = %v, want %v", got, want)
				}
				if name == "path" {
					if got, want := ShellPathUnknownForPlatform(semantic, platform), platform != runners.MacOrLinux; got != want {
						t.Fatalf("inserted PATH uncertainty = %v, want %v", got, want)
					}
				}
			})
		}
	}
}

func TestEnvironmentMappingPreservesSourceScalarTags(t *testing.T) {
	t.Parallel()
	workflow, errs := parser.ParseWorkflow([]byte("on: push\nenv:\n  NULL_VALUE: null\n  STRING: 'null'\n  BOOL: false\n  BOOL_TEXT: 'false'\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	resolved := mapping.Resolve(definition.EnvMappingView(workflow.Env))
	if got := resolved.Lookup("NULL_VALUE"); !got.Known() || got.Kind() != value.Null {
		t.Fatalf("unquoted null env state=%v kind=%v", got.State(), got.Kind())
	}
	if got := resolved.Lookup("STRING"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("quoted null env state=%v kind=%v", got.State(), got.Kind())
	}
	if got, known := resolved.Lookup("BOOL").Bool(); !known || got {
		t.Fatalf("unquoted false env=%v known=%v", got, known)
	}
	if got := resolved.Lookup("BOOL_TEXT"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("quoted false env state=%v kind=%v", got.State(), got.Kind())
	}
}

func TestCanonicalEnvironmentMappingPreservesSourceScalarTags(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\nenv:\n  NULL_VALUE: null\n  STRING: 'null'\n  BOOL: false\n  BOOL_TEXT: 'false'\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	parsed := parsing.Workflow(content)
	if len(parsed.SyntaxErrors) != 0 {
		t.Fatal(parsed.SyntaxErrors)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("workflow.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	workflow, present := snapshot.WorkflowView()
	if !present {
		t.Fatal("workflow view unavailable")
	}
	resolved := workflows.Environment(workflow.Env)
	if got := resolved.Lookup("NULL_VALUE"); !got.Known() || got.Kind() != value.Null {
		t.Fatalf("canonical unquoted null state=%v kind=%v", got.State(), got.Kind())
	}
	if got := resolved.Lookup("STRING"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("canonical quoted null state=%v kind=%v", got.State(), got.Kind())
	}
	if got, known := resolved.Lookup("BOOL").Bool(); !known || got {
		t.Fatalf("canonical unquoted false=%v known=%v", got, known)
	}
	if got := resolved.Lookup("BOOL_TEXT"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("canonical quoted false state=%v kind=%v", got.State(), got.Kind())
	}
}

package execution

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestCheckoutEnvironmentPlatform(t *testing.T) {
	for _, platform := range []runnerlang.Platform{runnerlang.Windows, runnerlang.MacOrLinux, runnerlang.Any} {
		for _, name := range []string{"git_work_tree", "path", "Node_Options", "ssh_askpass", "git_allow_protocol"} {
			t.Run(fmt.Sprintf("%s/%d", name, platform), func(t *testing.T) {
				env := &workflowlang.Env{Vars: map[string]*workflowlang.EnvVar{name: {Name: &workflowlang.String{Value: name}, Value: &workflowlang.String{Value: "override"}}}}
				semantic := mapping.Resolve(workflowlang.EnvMappingView(env))
				if got, want := CheckoutEnvironmentUnknown(semantic, platform), platform != runnerlang.MacOrLinux; got != want {
					t.Fatalf("checkout uncertainty = %v, want %v", got, want)
				}
				expression := &workflowlang.Env{Insertions: []*workflowlang.Env{{Expression: &workflowlang.String{Value: fmt.Sprintf("${{ fromJSON('{%q:\"override\"}') }}", name)}}}}
				semantic = mapping.Resolve(workflowlang.EnvMappingView(expression))
				if got, want := CheckoutEnvironmentUnknown(semantic, platform), platform != runnerlang.MacOrLinux; got != want {
					t.Fatalf("inserted checkout uncertainty = %v, want %v", got, want)
				}
				if name == "path" {
					if got, want := ShellPathUnknownForPlatform(semantic, platform), platform != runnerlang.MacOrLinux; got != want {
						t.Fatalf("inserted PATH uncertainty = %v, want %v", got, want)
					}
				}
			})
		}
	}
}

func TestEnvironmentMappingPreservesSourceScalarTags(t *testing.T) {
	workflow, errs := workflowlang.Parse([]byte("on: push\nenv:\n  NULL_VALUE: null\n  STRING: 'null'\n  BOOL: false\n  BOOL_TEXT: 'false'\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	resolved := mapping.Resolve(workflowlang.EnvMappingView(workflow.Env))
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
	content := []byte("on: push\nenv:\n  NULL_VALUE: null\n  STRING: 'null'\n  BOOL: false\n  BOOL_TEXT: 'false'\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	parsed := documentparse.Workflow("workflow.yml", content)
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
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
	resolved := workflow.Env().Mapping()
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

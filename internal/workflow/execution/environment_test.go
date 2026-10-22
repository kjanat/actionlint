package execution

import (
	"fmt"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

func TestCheckoutEnvironmentPlatform(t *testing.T) {
	for _, platform := range []runnerlang.Platform{runnerlang.Windows, runnerlang.MacOrLinux, runnerlang.Any} {
		for _, name := range []string{"git_work_tree", "path", "Node_Options", "ssh_askpass", "git_allow_protocol"} {
			t.Run(fmt.Sprintf("%s/%d", name, platform), func(t *testing.T) {
				env := &workflowlang.Env{Vars: map[string]*workflowlang.EnvVar{name: {Name: &workflowlang.String{Value: name}, Value: &workflowlang.String{Value: "override"}}}}
				if got, want := CheckoutEnvironmentUnknown(env, platform), platform != runnerlang.MacOrLinux; got != want {
					t.Fatalf("checkout uncertainty = %v, want %v", got, want)
				}
				expression := &workflowlang.Env{Insertions: []*workflowlang.Env{{Expression: &workflowlang.String{Value: fmt.Sprintf("${{ fromJSON('{%q:\"override\"}') }}", name)}}}}
				if got, want := CheckoutEnvironmentUnknown(expression, platform), platform != runnerlang.MacOrLinux; got != want {
					t.Fatalf("inserted checkout uncertainty = %v, want %v", got, want)
				}
				if name == "path" {
					if got, want := ShellPathUnknownForPlatform(expression, platform), platform != runnerlang.MacOrLinux; got != want {
						t.Fatalf("inserted PATH uncertainty = %v, want %v", got, want)
					}
				}
			})
		}
	}
}

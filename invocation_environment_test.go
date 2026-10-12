package actionlint

import (
	"fmt"
	"testing"
)

func TestCheckoutEnvironmentPlatform(t *testing.T) {
	for _, platform := range []platformKind{platformKindWindows, platformKindMacOrLinux, platformKindAny} {
		for _, name := range []string{"git_work_tree", "path", "Node_Options", "ssh_askpass", "git_allow_protocol"} {
			t.Run(fmt.Sprintf("%s/%d", name, platform), func(t *testing.T) {
				env := &Env{Vars: map[string]*EnvVar{name: {Name: &String{Value: name}, Value: &String{Value: "override"}}}}
				if got, want := checkoutEnvironmentUnknown(env, platform), platform != platformKindMacOrLinux; got != want {
					t.Fatalf("checkout uncertainty = %v, want %v", got, want)
				}
			})
		}
	}
}

package managed

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestSourceGrantsTranslateScriptsAndInvokedRuntimeWithoutMutatingFacts(t *testing.T) {
	scriptRuntime := &scriptmodel.Execution{Directory: "script-host", DirectoryKnown: true}
	invokedRuntime := &scriptmodel.Execution{Directory: "invoked-host", DirectoryKnown: true}
	input := checkinputs.Input{
		Scripts: []checkinputs.Script{{Runtime: scriptRuntime}, {}},
		Invoked: []checkinputs.Script{{Runtime: invokedRuntime}, {}},
	}
	files := pluginSourceFiles{mounts: []pluginSourceMount{{host: "host", guest: "/workspace"}}, directory: "/workspace"}
	files.configure(&input)
	for name, scripts := range map[string][]checkinputs.Script{"scripts": input.Scripts, "invoked": input.Invoked} {
		if scripts[0].Runtime == nil || scripts[0].Runtime.Directory != "/workspace" || !scripts[0].Runtime.DirectoryKnown || scripts[1].Runtime != nil {
			t.Errorf("%s runtime was not translated: %+v", name, scripts)
		}
	}
	if scriptRuntime.Directory != "script-host" || invokedRuntime.Directory != "invoked-host" {
		t.Fatalf("translation mutated source runtimes: script=%+v invoked=%+v", scriptRuntime, invokedRuntime)
	}
}

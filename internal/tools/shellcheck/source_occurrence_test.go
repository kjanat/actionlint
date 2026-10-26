package shellcheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestSourceGrantsTranslateScriptsAndInvokedRuntimeWithoutMutatingFacts(t *testing.T) {
	scriptRuntime := &analyzer.Runtime{Directory: "script-host", DirectoryKnown: true}
	invokedRuntime := &analyzer.Runtime{Directory: "invoked-host", DirectoryKnown: true}
	input := analyzer.Input{
		Scripts: []analyzer.Script{{Runtime: scriptRuntime}, {}},
		Invoked: []analyzer.Script{{Runtime: invokedRuntime}, {}},
	}
	files := pluginSourceFiles{mounts: []pluginSourceMount{{host: "host", guest: "/workspace"}}, directory: "/workspace"}
	files.configure(&input)
	for name, scripts := range map[string][]analyzer.Script{"scripts": input.Scripts, "invoked": input.Invoked} {
		if scripts[0].Runtime == nil || scripts[0].Runtime.Directory != "/workspace" || !scripts[0].Runtime.DirectoryKnown || scripts[1].Runtime != nil {
			t.Errorf("%s runtime was not translated: %+v", name, scripts)
		}
	}
	if scriptRuntime.Directory != "script-host" || invokedRuntime.Directory != "invoked-host" {
		t.Fatalf("translation mutated retained runtimes: script=%+v invoked=%+v", scriptRuntime, invokedRuntime)
	}
}

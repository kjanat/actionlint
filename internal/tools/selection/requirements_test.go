package selection

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/internal/config"
)

func TestConfigCandidatesRetainDistinctTransports(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(fmt.Sprintf(`overrides:
  - includes: ['process-action/**']
    tools: {%s: {backend: {process: {executable: checker}}}}
  - includes: ['wasm-action/**']
    tools: {%s: {backend: {wasm: {path: checker.wasm}}}}
`, name, name)))
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := ConfigCandidates(name, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var hasDefault, hasProcess, hasWasm bool
			for _, candidate := range candidates {
				backend := candidate.Tools.Shellcheck.Backend
				if name == "ruff" {
					backend = candidate.Tools.Ruff.Backend
				}
				switch {
				case backend == nil:
					hasDefault = true
				case backend.Process != nil:
					hasProcess = true
				case backend.Wasm != nil:
					hasWasm = true
				}
			}
			if !hasDefault || !hasProcess || !hasWasm {
				t.Fatalf("transport candidates lost: default=%t process=%t wasm=%t", hasDefault, hasProcess, hasWasm)
			}
		})
	}
}

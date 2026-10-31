package backend

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
)

func TestConfigCandidatesRetainDistinctTransports(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse(fmt.Appendf(nil, `overrides:
  - includes: ['process-action/**']
    tools: {%s: {backend: {process: {executable: checker}}}}
  - includes: ['wasm-action/**']
    tools: {%s: {backend: {wasm: {path: checker.wasm}}}}
`, name, name))
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := ConfigCandidates(name, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var hasDefault, hasProcess, hasWasm bool
			for _, candidate := range candidates {
				backend := candidate.Tools["shellcheck"].Backend
				if name == "ruff" {
					backend = candidate.Tools["ruff"].Backend
				}
				switch {
				case backend == nil:
					hasDefault = true
				case hasTransport(backend.Process()):
					hasProcess = true
				case hasTransport(backend.Wasm()):
					hasWasm = true
				}
			}
			if !hasDefault || !hasProcess || !hasWasm {
				t.Fatalf("transport candidates lost: default=%t process=%t wasm=%t", hasDefault, hasProcess, hasWasm)
			}
			needed, err := ProjectMayNeedProcessFor(name, cfg, nil, false, func(candidate *config.Config) bool {
				backend := candidate.Tools["shellcheck"].Backend
				if name == "ruff" {
					backend = candidate.Tools["ruff"].Backend
				}
				return hasTransport(backend.Process())
			})
			if err != nil || !needed {
				t.Fatalf("host plugin default hid an explicit process: needed=%t error=%v", needed, err)
			}
		})
	}
}

// hasTransport reports whether a Backend accessor found its transport.
func hasTransport[T any](_ T, ok bool) bool { return ok }

package ruff

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

type planningBackend struct{}

func (planningBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return nil, nil
}

func TestPluginFallbackPlansCompositeProcessOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         bool
	}{
		{"plugin default", "{}", false},
		{"composite process", "overrides: [{includes: ['actions/**'], tools: {ruff: {backend: {process: {executable: ruff}}}}}]", true},
		{"composite wasm", "overrides: [{includes: ['actions/**'], tools: {ruff: {backend: {wasm: {path: engine.wasm}}}}}]", false},
		{"process disabled", "overrides: [{includes: ['actions/**'], tools: {ruff: {enabled: false, backend: {process: {executable: ruff}}}}}]", false},
		{"process superseded", "overrides: [{includes: ['actions/**'], tools: {ruff: {backend: {process: {executable: ruff}}}}}, {includes: ['**'], tools: {ruff: {backend: {wasm: {path: engine.wasm}}}}}]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			registration, err := (Options{Command: "ruff", Process: &processconfig.Options{Optional: true}}).Registration().WithBackend(planningBackend{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := registration.NeedsProcess(t.Context(), cfg, cfg, true)
			if err != nil || got != tc.want {
				t.Fatalf("needed=%v, want=%v, error=%v", got, tc.want, err)
			}
		})
	}
}

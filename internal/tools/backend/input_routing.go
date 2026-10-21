package backend

import (
	"context"
	"errors"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/tools"
)

// WithScriptInputs retains native run preparation and adds the engine's input-only route.
func WithScriptInputs(registration tools.Registration, descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend) (tools.Registration, error) {
	if len(descriptor.ScriptInputs) == 0 {
		return registration, nil
	}
	descriptor.Shells = nil
	inputs, err := registerEngine(descriptor, backend, registration.Enabled, registration.PrepareInput)
	if err != nil {
		return tools.Registration{}, err
	}
	build := registration.Build
	registration.ScriptInputs = slices.Concat(registration.ScriptInputs, inputs.ScriptInputs)
	registration.Build = func(c tools.Context) (tools.Runner, error) {
		native, err := build(c)
		if err != nil {
			return nil, err
		}
		input, err := inputs.Build(c)
		if err != nil {
			return nil, err
		}
		return inputRouting{native: native, input: input}, nil
	}
	return registration, nil
}

type inputRouting struct{ native, input tools.Runner }

func (r inputRouting) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	native, nativeErr := r.native.Analyze(ctx, batch)
	input, inputErr := r.input.Analyze(ctx, batch)
	return slices.Concat(native, input), errors.Join(nativeErr, inputErr)
}

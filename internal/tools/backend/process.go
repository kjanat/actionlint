package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/rule"
)

// Process registers a native provider speaking the same check request/result
// envelope as a core-WASM engine. Native CLI-specific protocols need an adapter.
func Process(descriptor rule.Descriptor, shells []string, executable string, options *processconfig.Options) tools.Registration {
	return tools.Registration{
		Descriptor: descriptor, RequiresExecution: true,
		NeedsProcess: func(_ *config.Config, _ *config.Config, _ bool) (bool, error) { return true, nil },
		Build: func(c tools.Context) (tools.Runner, error) {
			command, err := c.Processes.ConfiguredCommandRunner(executable, options, false)
			if err != nil {
				return nil, err
			}
			command.RequireSuccess = true
			registration, err := Register(descriptor, shells, processBackend{command})
			if err != nil {
				return nil, err
			}
			return registration.Build(c)
		},
	}
}

type processBackend struct{ command *analyzerhost.Command }

func (p processBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	output, err := p.command.Execute(ctx, nil, string(encoded))
	if err != nil {
		return nil, err
	}
	var response struct {
		OK          *bool                       `json:"ok"`
		Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
		Errors      []string                    `json:"errors"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode tool response: %w", err)
	}
	if response.OK == nil {
		return nil, errors.New("tool response is missing ok")
	}
	var failures error
	for _, message := range response.Errors {
		failures = errors.Join(failures, errors.New(message))
	}
	if !*response.OK && failures == nil {
		failures = errors.New("tool reported failure without an explanation")
	}
	return response.Diagnostics, failures
}

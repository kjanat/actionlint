package lsp

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
)

type pythonInputBackend struct{}

func (pythonInputBackend) TemplateContract() string { return inputs.TemplateEffectsContract }

func (pythonInputBackend) Check(_ context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
	var findings []inputs.ScriptDiagnostic
	for _, match := range regexp.MustCompile(`_[a-z0-9_]*|missing`).FindAllStringIndex(request.Source, -1) {
		before := request.Source[:match[0]]
		row := strings.Count(before, "\n") + 1
		column := match[0] - strings.LastIndex(before, "\n")
		findings = append(findings, inputs.ScriptDiagnostic{
			Filename: request.Filename, Code: "F821", Message: "undefined name",
			Location:       inputs.ScriptPosition{Row: row, Column: column},
			EndLocation:    inputs.ScriptPosition{Row: row, Column: column + match[1] - match[0]},
			TemplateEffect: inputs.TemplateEffectOpaqueName,
		})
	}
	return findings, nil
}

type inputReplacementBackend struct {
	mu       sync.Mutex
	requests []inputs.ScriptRequest
}

func (b *inputReplacementBackend) Check(_ context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, request)
	return nil, nil
}

func (*inputReplacementBackend) TemplateContract() string { return inputs.TemplateEffectsContract }

type selectedInputBackend struct {
	inputReplacementBackend
	descriptor inputs.ScriptEngineDescriptor
}

func (b *selectedInputBackend) ScriptDescriptor() inputs.ScriptEngineDescriptor {
	return b.descriptor
}

package lsp

import (
	"context"
	"regexp"
	"strings"
	"sync"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

type pythonInputBackend struct{}

func (pythonInputBackend) TemplateContract() string { return checkinputs.TemplateEffectsContract }

func (pythonInputBackend) Check(_ context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	var findings []checkinputs.ScriptDiagnostic
	for _, match := range regexp.MustCompile(`_[a-z0-9_]*|missing`).FindAllStringIndex(request.Source, -1) {
		before := request.Source[:match[0]]
		row := strings.Count(before, "\n") + 1
		column := match[0] - strings.LastIndex(before, "\n")
		findings = append(findings, checkinputs.ScriptDiagnostic{
			Filename: request.Filename, Code: "F821", Message: "undefined name",
			Location:       checkinputs.ScriptPosition{Row: row, Column: column},
			EndLocation:    checkinputs.ScriptPosition{Row: row, Column: column + match[1] - match[0]},
			TemplateEffect: checkinputs.TemplateEffectOpaqueName,
		})
	}
	return findings, nil
}

type inputReplacementBackend struct {
	mu       sync.Mutex
	requests []checkinputs.ScriptRequest
}

func (b *inputReplacementBackend) Check(_ context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, request)
	return nil, nil
}

func (*inputReplacementBackend) TemplateContract() string { return checkinputs.TemplateEffectsContract }

type selectedInputBackend struct {
	inputReplacementBackend
	descriptor checkinputs.ScriptEngineDescriptor
}

func (b *selectedInputBackend) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	return b.descriptor
}

package lsp

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

type pythonInputBackend struct{}

func (pythonInputBackend) TemplateContract() string { return scriptengine.TemplateEffectsContract }

func (pythonInputBackend) Check(_ context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	var findings []scriptengine.Diagnostic
	for _, match := range regexp.MustCompile(`_[a-z0-9_]*|missing`).FindAllStringIndex(request.Source, -1) {
		before := request.Source[:match[0]]
		row := strings.Count(before, "\n") + 1
		column := match[0] - strings.LastIndex(before, "\n")
		findings = append(findings, scriptengine.Diagnostic{
			Filename: request.Filename, Code: "F821", Message: "undefined name",
			Location:       sourcetext.Position{Line: row, Column: column},
			EndLocation:    sourcetext.Position{Line: row, Column: column + match[1] - match[0]},
			TemplateEffect: scriptengine.TemplateEffectOpaqueName,
		})
	}
	return findings, nil
}

type inputReplacementBackend struct {
	mu       sync.Mutex
	requests []scriptengine.Request
}

func (b *inputReplacementBackend) Check(_ context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, request)
	return nil, nil
}

func (*inputReplacementBackend) TemplateContract() string {
	return scriptengine.TemplateEffectsContract
}

type selectedInputBackend struct {
	inputReplacementBackend
	descriptor scriptengine.Descriptor
}

func (b *selectedInputBackend) ScriptDescriptor() scriptengine.Descriptor {
	return b.descriptor
}

package engine

import (
	"io"

	"actionlint.kjanat.dev/internal/render"
)

// AnalysisRenderer formats analysis results and retains custom-template rule metadata across calls.
type AnalysisRenderer struct {
	renderer *render.Renderer
}

// NewAnalysisRenderer validates a built-in format or custom Go template before rendering.
func NewAnalysisRenderer(format OutputFormat, template string, oneline bool) (*AnalysisRenderer, error) {
	renderer, err := render.New(format, template, oneline, Version, templateRules())
	if err != nil {
		return nil, err
	}
	return &AnalysisRenderer{renderer}, nil
}

// Render writes findings using their original sources and the selected output format.
func (r *AnalysisRenderer) Render(out io.Writer, result *AnalysisResult) error {
	return r.renderer.Render(out, result.RenderResult())
}

// RenderResult snapshots source-aware findings for presentation outside the engine.
func (r *AnalysisResult) RenderResult() render.Result {
	rendered := render.Result{
		FileCount: r.FileCount(), Diagnostics: r.Diagnostics, Documents: r.Documents, Configurations: r.Configurations,
		Findings: make([]render.Finding, 0, len(r.Diagnostics)),
		Rules:    templateRules(),
	}
	index := 0
	for _, file := range r.files {
		for _, rule := range file.rules {
			rendered.Rules = append(rendered.Rules, render.Rule{Name: rule.Name, Description: rule.Description})
		}
		for _, original := range file.errors {
			finding := render.FindingOf(r.Diagnostics[index], original.sourceFor(file.source.Content))
			finding.RawScript = original.rawScriptSource
			rendered.Findings = append(rendered.Findings, finding)
			index++
		}
	}
	return rendered
}

// CheckResult exports a completed analysis using the shared public result contract.
func (r *AnalysisResult) CheckResult() CheckResult {
	return r.RenderResult().CheckResult()
}

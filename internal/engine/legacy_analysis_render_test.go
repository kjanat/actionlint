package engine

import (
	"io"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/render"
)

// AnalysisRenderer formats analysis results and retains custom-template rule metadata across calls.
type AnalysisRenderer struct {
	renderer *render.Renderer
}

// NewAnalysisRenderer validates a built-in format or custom Go template before rendering.
func NewAnalysisRenderer(format render.Format, template string, oneline bool) (*AnalysisRenderer, error) {
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
	return render.Prepare(r.Analysis)
}

// CheckResult exports a completed analysis using the shared public result contract.
func (r *AnalysisResult) CheckResult() render.CheckResult {
	return r.RenderResult().CheckResult()
}

func (r *AnalysisResult) legacyErrors() []*findingmodel.Finding {
	if len(r.files) == 1 {
		return r.files[0].errors
	}
	errors := make([]*findingmodel.Finding, 0, len(r.Diagnostics))
	for _, file := range r.files {
		errors = append(errors, file.errors...)
	}
	return errors
}

func CompileIgnorePatterns(patterns []string) (IgnorePatterns, error) {
	return repoconfig.CompileIgnorePatterns(patterns)
}

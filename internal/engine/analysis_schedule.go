package engine

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	repoconfig "actionlint.kjanat.dev/internal/config"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

// runScriptAnalyzers schedules prepared work without knowing how scripts were discovered.
func runScriptAnalyzers(ctx context.Context, analyzers []scriptAnalyzer, batch scriptBatch) ([]*findingmodel.Finding, error) {
	tasks := make([]analyzerhost.Task[*findingmodel.Finding], len(analyzers))
	for i, builtin := range analyzers {
		tasks[i] = func(ctx context.Context) ([]*findingmodel.Finding, error) { return builtin.AnalyzeScripts(ctx, batch) }
	}
	return analyzerhost.RunTasks(ctx, tasks)
}
func runDocumentAnalyzers(ctx context.Context, registrations []analyzer.Registration, input analyzer.Input, metadataSources map[string][]byte, usedRules *[]RuleInfo) ([]*findingmodel.Finding, error) {
	var all []*findingmodel.Finding
	var analysisErr error
	if len(registrations) != 0 {
		findings, err := analyzerhost.Run(ctx, input, registrations)
		analysisErr = err
		for _, registration := range registrations {
			if registration.Analyzer != nil {
				*usedRules = append(*usedRules, registration.Analyzer.Descriptor().Clone())
			}
		}
		for _, finding := range findings {
			legacy := findingmodel.FromDiagnostic(finding)
			if legacy.Path != "" && legacy.Path != input.Path {
				if source, ok := metadataSources[legacy.Path]; ok {
					legacy.Source = source
				}
			}
			all = append(all, legacy)
		}
	}

	return all, analysisErr
}

// Explicit registration enables a plugin by default; presets and per-rule settings
// still apply. Library registration options take precedence over repository options.
func configuredAnalyzers(config *repoconfig.Config, registrations []analyzer.Registration) ([]analyzer.Registration, error) {
	selected := make([]analyzer.Registration, 0, len(registrations))
	for _, registration := range registrations {
		name := registration.Analyzer.Descriptor().Name
		if repoconfig.DiagnosticLevel(config, name) == "off" {
			continue
		}
		if config != nil && len(registration.Options) == 0 {
			if options := config.Lint.Rules.Plugins.Rules[name].Options; options != nil {
				data, err := jsonv2.Marshal(options)
				if err != nil {
					return nil, fmt.Errorf("options for analyzer %q: %w", name, err)
				}
				registration.Options = data
			}
		}
		selected = append(selected, registration)
	}
	return selected, nil
}

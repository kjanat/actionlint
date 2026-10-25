package component

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"github.com/kjanat/wacogo"
)

func (p *Plugin) HasAnalyzer() bool { return p.analyzerDescriptor != nil }

func (p *Plugin) loadAnalyzer(ctx context.Context) error {
	descriptor, err := p.describe(ctx)
	if err != nil {
		return err
	}
	if descriptor.Name == "" {
		return errors.New("plugin describes itself with an empty name")
	}
	if err := validateSuppressionRuleName(descriptor.Name); err != nil {
		return err
	}
	p.analyzerDescriptor = &descriptor
	return nil
}

// Descriptor returns the identity the plugin declared when it was loaded.
func (p *Plugin) Descriptor() analyzer.Descriptor {
	if p.analyzerDescriptor == nil {
		return analyzer.Descriptor{}
	}
	return analyzer.Descriptor{Descriptor: p.analyzerDescriptor.Clone(), Requires: append([]analyzer.Fact(nil), p.analyzerDescriptor.Requires...)}
}

// Analyze runs the plugin on one document in a fresh instance. A trap, an
// exit, an exceeded limit or a refused call is returned as an error.
func (p *Plugin) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if !p.HasAnalyzer() {
		return nil, errors.New("component does not implement the analyzer role")
	}
	ctx = context.WithValue(ctx, fileSystemsKey{}, input.Files)
	ctx = context.WithValue(ctx, sourceRootsKey{}, input.SourceRoots)
	ctx = context.WithValue(ctx, preparedScriptsKey{}, preparationScope{prepare: input.PrepareScript, references: suppliedComments(input)})
	ctx = context.WithValue(ctx, sourceCommentsKey{}, suppliedComments(input))
	value, err := p.callAnalyzer(ctx, "analyze", lowerInput(input))
	if err != nil {
		return nil, err
	}
	diagnostics, err := liftAnalysis(value)
	valid := diagnostics[:0]
	for _, finding := range diagnostics {
		if finding.Rule != "" && finding.Rule != p.analyzerDescriptor.Name {
			err = errors.Join(err, fmt.Errorf("undeclared diagnostic rule %q; use code for plugin-specific check identifiers", finding.Rule))
			continue
		}
		valid = append(valid, finding)
	}
	if err != nil {
		return valid, p.failure(ctx, err)
	}
	return valid, nil
}

func (p *Plugin) describe(ctx context.Context) (analyzer.Descriptor, error) {
	value, err := p.callAnalyzer(ctx, "describe")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	descriptor, err := liftDescriptor(value)
	if err != nil {
		return analyzer.Descriptor{}, p.failure(ctx, err)
	}
	return descriptor, nil
}

func (p *Plugin) callAnalyzer(ctx context.Context, name string, args ...wacogo.Val) (wacogo.Val, error) {
	return p.callInterface(ctx, AnalyzerInterface, name, args...)
}

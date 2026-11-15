package results_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	source "actionlint.kjanat.dev/v2/internal/input/source"
	facts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

// preparePublicationBatch binds the configuration of each source before sealing.
func preparePublicationBatch(t *testing.T, batch policy.Batch, caller *config.Config, sourceConfigs map[string]*config.Config) policy.Batch {
	t.Helper()
	return preparePublicationBatchAt(t, batch, caller, sourceConfigs, t.TempDir())
}

func preparePublicationBatchAt(t *testing.T, batch policy.Batch, caller *config.Config, sourceConfigs map[string]*config.Config, captureRoot string) policy.Batch {
	t.Helper()
	rules := checking.ConfigurationRules()
	registry := binding.NewRegistry(rules)
	batch.Processor = policy.Processor{RuleSet: rules}
	if batch.Policy.Path == "" {
		batch.Policy.Path = batch.InputPath
	}
	if batch.Policy.ConfigPath == "" {
		batch.Policy.ConfigPath = batch.InputPath
	}
	if batch.References == nil {
		batch.References = make(map[string]facts.SourceIndex)
	}
	if batch.Policy.Content != nil || len(batch.Sources) > 0 {
		_, fixture := capturefixture.NewCapturedSources(t, captureRoot)
		admit := func(path string, content []byte) {
			if path == "" {
				return
			}
			snapshot, err := fixture.Capture(path, content, source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			rev := snapshot.Revision()
			batch.References[path] = facts.SourceIndex{Document: source.Reference{Document: rev.Document(), Revision: rev.Digest()}}
		}
		if batch.Policy.Content != nil {
			admit(batch.Policy.ConfigPath, batch.Policy.Content)
		}
		for path, content := range batch.Sources {
			admit(path, content)
		}
	}
	batch.Policy.Scope = registry.Prepare(caller, caller, caller)
	batch.Policy.SourceBindings = make(map[string]*binding.Scope, len(sourceConfigs))
	for path, cfg := range sourceConfigs {
		batch.Policy.SourceBindings[path] = registry.Prepare(caller, cfg, cfg)
	}
	owned := make([]*finding.Finding, len(batch.Findings))
	for i, f := range batch.Findings {
		copyOfFinding := *f
		f = &copyOfFinding
		owned[i] = f
		scope := batch.Policy.Scope
		if selected, ok := batch.Policy.SourceBindings[f.Path]; ok {
			scope = selected
		}
		f.Binding = scope.Rule(f.Rule).ID()
	}
	batch.Findings = owned
	batch.Policy.Bindings = registry.Seal()
	return batch
}

func publishFindings(t *testing.T, p policy.Policy, cfg *config.Config, findings []*finding.Finding, sources map[string][]byte, sourceConfigs map[string]*config.Config) ([]*finding.Finding, error) {
	t.Helper()
	batch := preparePublicationBatch(t, policy.Batch{Policy: p, InputPath: p.Path, Findings: findings, Sources: sources}, cfg, sourceConfigs)
	result, err := policy.Publish([]policy.Batch{batch}, nil)
	if len(result.Findings) == 0 {
		return nil, err
	}
	return result.Findings[0], err
}

func publishBatches(t *testing.T, batches []policy.Batch) policy.Publication {
	t.Helper()
	captureRoot := t.TempDir()
	for i := range batches {
		sourceConfigs := map[string]*config.Config{}
		for _, item := range batches[i].Findings {
			if item.Path == "" || item.Source == nil || item.UnavailableSource.Present() {
				continue
			}
			if batches[i].Sources == nil {
				batches[i].Sources = map[string][]byte{}
			}
			batches[i].Sources[item.Path] = item.Source
			sourceConfigs[item.Path] = nil
		}
		batches[i] = preparePublicationBatchAt(t, batches[i], nil, sourceConfigs, captureRoot)
	}
	out, err := policy.Publish(batches, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

package policy_test

import (
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func publishSuppressionFixture(t *testing.T, p policy.DocumentInputs, cfg *configuration.Config, findings []*finding.Finding, sources map[string][]byte) []*finding.Finding {
	t.Helper()
	rules := checking.ConfigurationRules()
	groups := map[string]any{}
	if !slices.Contains(fixtureRuleNames(findings), "unused-suppression") {
		groups["disable"] = []string{"unused-suppression"}
	}
	for _, name := range append([]string{"inline-suppression", "forbidden-suppression"}, fixtureRuleNames(findings)...) {
		d, ok := rules.FindRule(name)
		if !ok {
			continue
		}
		entries, ok := groups[d.Category].(map[string]any)
		if !ok {
			entries = map[string]any{}
			groups[d.Category] = entries
		}
		entries[d.Name] = "on"
	}
	raw, err := json.Marshal(map[string]any{"lint": map[string]any{"rules": groups}})
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := rules.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		cfg = &configuration.Config{}
	} else {
		owned := *cfg
		cfg = &owned
	}
	cfg.Lint.Rules = enabled.Lint.Rules
	originalRestrictions := cfg.Policy.DisallowSuppressions
	cfg, err = rules.ResolveRules(configuration.Presets{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Policy.DisallowSuppressions = originalRestrictions
	sourceConfigs := map[string]*configuration.Config{}
	for path := range sources {
		sourceConfigs[path] = cfg
	}
	p = preparePolicy(rules, cfg, p, sourceConfigs, findings)
	got, err := publishFindings(policy.Processor{RuleSet: rules}, p, findings, sources)
	if err != nil {
		t.Fatal(err)
	}
	ordered := make([]*finding.Finding, 0, len(got))
	used := make([]bool, len(got))
	for _, original := range findings {
		for i, candidate := range got {
			if !used[i] && candidate.Rule == original.Rule && candidate.Start == original.Start && candidate.Message == original.Message {
				ordered = append(ordered, candidate)
				used[i] = true
				break
			}
		}
	}
	for i, candidate := range got {
		if !used[i] {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

func fixtureRuleNames(findings []*finding.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Rule)
	}
	return out
}

func fixtureInline(t *testing.T, source []byte, findings []*finding.Finding, restrictions *configuration.SuppressionsPolicy, additional ...string) []*finding.Finding {
	t.Helper()
	cfg := &configuration.Config{}
	cfg.Policy.DisallowSuppressions = restrictions
	return publishSuppressionFixture(t, policy.DocumentInputs{Path: "fixture.yml", ConfigPath: "fixture.yml", Content: source, AdditionalRules: additional}, cfg, findings, nil)
}

func fixtureForeign(t *testing.T, path string, source []byte, findings []*finding.Finding, restrictions *configuration.SuppressionsPolicy) []*finding.Finding {
	t.Helper()
	cfg := &configuration.Config{}
	cfg.Policy.DisallowSuppressions = restrictions
	return publishSuppressionFixture(t, policy.DocumentInputs{Path: "caller.yml", ConfigPath: "caller.yml"}, cfg, findings, map[string][]byte{path: source})
}

func fixtureConfigured(t *testing.T, path, root string, source []byte, findings []*finding.Finding, cfg *configuration.Config) []*finding.Finding {
	t.Helper()
	return publishSuppressionFixture(t, policy.DocumentInputs{Path: path, ConfigPath: path, Root: root, Content: source}, cfg, findings, nil)
}

type fixtureIdentity struct {
	Rule    string
	Start   sourcetext.Position
	Message string
	Source  string
}

func fixtureIdentities(findings []*finding.Finding) []fixtureIdentity {
	out := make([]fixtureIdentity, len(findings))
	for i, f := range findings {
		out[i] = fixtureIdentity{Rule: f.Rule, Start: f.Start, Message: f.Message, Source: string(f.Source.Content)}
	}
	return out
}

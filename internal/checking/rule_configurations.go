package checking

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// ruleConfigurations prepares each source and action scope once for this check.
// Execution and result processing receive these same resolved configurations.
type ruleConfigurations struct {
	project          *repoconfig.Config
	root             string
	presets          repoconfig.Presets
	registrations    []repoconfig.RuleRegistration
	rules            repoconfig.RuleSet
	declarationError error
	sources          map[string]*repoconfig.Config
	actions          map[string]*repoconfig.Config
}

func newRuleConfigurations(project *repoconfig.Config, root string, presets repoconfig.Presets, registrations []repoconfig.RuleRegistration) *ruleConfigurations {
	rules, err := ConfigurationRules().WithRegisteredRules(registrations...)
	return &ruleConfigurations{project: project, root: root, presets: presets, registrations: registrations, rules: rules, declarationError: err, sources: map[string]*repoconfig.Config{}, actions: map[string]*repoconfig.Config{}}
}

func (c *ruleConfigurations) resolve(config *repoconfig.Config, err error) (*repoconfig.Config, error) {
	if c.declarationError != nil {
		return nil, c.declarationError
	}
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = &repoconfig.Config{}
	}
	return ConfigurationRules().ResolveRegisteredRules(c.presets, config, c.registrations...)
}

func (c *ruleConfigurations) source(path string) (*repoconfig.Config, error) {
	if config, found := c.sources[path]; found {
		return config, nil
	}
	config, err := c.resolve(c.rules.ForSuppressions(c.project, path, c.root))
	if err != nil {
		return nil, err
	}
	c.sources[path] = config
	return config, nil
}

func (c *ruleConfigurations) action(path string) (*repoconfig.Config, error) {
	if config, found := c.actions[path]; found {
		return config, nil
	}
	if _, err := c.source(path); err != nil {
		return nil, err
	}
	config, err := c.resolve(c.rules.ForAction(c.project, path, c.root))
	if err != nil {
		return nil, err
	}
	c.actions[path] = config
	return config, nil
}

func (c *ruleConfigurations) workflow(path string) (*repoconfig.Config, error) {
	return c.resolve(c.rules.ForFile(c.project, path, c.root))
}

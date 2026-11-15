package checking

import (
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
)

// ruleConfigurations prepares each source and action scope once for this check.
// Execution and result processing receive these same resolved configurations.
type ruleConfigurations struct {
	project        *configuration.Config
	root           string
	presets        configuration.Presets
	registrations  []configuration.RuleRegistration
	rules          configuration.RuleSet
	sources        map[string]*configuration.Config
	actions        map[string]*configuration.Config
	bindings       map[string]*binding.Scope
	entry          *configuration.Config
	registry       *binding.Registry
	remoteBindings map[remoteBinding]*binding.Scope
}

type remoteBinding struct {
	parent *binding.Scope
	path   string
}

func newRuleConfigurations(project *configuration.Config, root string, presets configuration.Presets, rules configuration.RuleSet, registrations []configuration.RuleRegistration) *ruleConfigurations {
	return &ruleConfigurations{project: project, root: root, presets: presets, registrations: registrations, rules: rules, sources: map[string]*configuration.Config{}, actions: map[string]*configuration.Config{}, bindings: map[string]*binding.Scope{}, registry: binding.NewRegistry(rules)}
}

func (c *ruleConfigurations) bindEntry(path string, config *configuration.Config) *binding.Scope {
	c.entry = config
	scope := c.registry.Prepare(config, config, config)
	c.bindings[path] = scope
	return scope
}

func (c *ruleConfigurations) bindSource(path string) (*binding.Scope, error) {
	if scope, found := c.bindings[path]; found {
		return scope, nil
	}
	source, err := c.source(path)
	if err != nil {
		return nil, err
	}
	composite, err := c.action(path)
	if err != nil {
		return nil, err
	}
	scope := c.registry.Prepare(c.entry, source, composite)
	c.bindings[path] = scope
	return scope, nil
}

func (c *ruleConfigurations) bindInvocation(parent *binding.Scope, path string) (*binding.Scope, error) {
	if !source.RemoteAddress(path) {
		return c.bindSource(path)
	}
	key := remoteBinding{parent, path}
	if scope, found := c.remoteBindings[key]; found {
		return scope, nil
	}
	settings, err := c.source(path)
	if err != nil {
		return nil, err
	}
	scope := c.registry.Prepare(c.entry, settings, parent.Composite())
	if c.remoteBindings == nil {
		c.remoteBindings = make(map[remoteBinding]*binding.Scope)
	}
	c.remoteBindings[key] = scope
	return scope, nil
}

func (c *ruleConfigurations) resolve(config *configuration.Config, err error) (*configuration.Config, error) {
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = &configuration.Config{}
	}
	return ConfigurationRules().ResolveRegisteredRules(c.presets, config, c.registrations...)
}

func (c *ruleConfigurations) source(path string) (*configuration.Config, error) {
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

func (c *ruleConfigurations) action(path string) (*configuration.Config, error) {
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

func (c *ruleConfigurations) workflow(path string) (*configuration.Config, error) {
	return c.resolve(c.rules.ForFile(c.project, path, c.root))
}

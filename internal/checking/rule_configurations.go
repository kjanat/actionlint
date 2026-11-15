package checking

import (
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
)

// ruleConfigurations prepares each source and action scope once for this check.
// Execution and result processing receive these same resolved configurations.
type ruleConfigurations struct {
	project        *repoconfig.Config
	root           string
	presets        repoconfig.Presets
	registrations  []repoconfig.RuleRegistration
	rules          repoconfig.RuleSet
	sources        map[string]*repoconfig.Config
	actions        map[string]*repoconfig.Config
	bindings       map[string]*binding.Scope
	entry          *repoconfig.Config
	registry       *binding.Registry
	remoteBindings map[remoteBinding]*binding.Scope
}

type remoteBinding struct {
	parent *binding.Scope
	path   string
}

func newRuleConfigurations(project *repoconfig.Config, root string, presets repoconfig.Presets, rules repoconfig.RuleSet, registrations []repoconfig.RuleRegistration) *ruleConfigurations {
	return &ruleConfigurations{project: project, root: root, presets: presets, registrations: registrations, rules: rules, sources: map[string]*repoconfig.Config{}, actions: map[string]*repoconfig.Config{}, bindings: map[string]*binding.Scope{}, registry: binding.NewRegistry(rules)}
}

func (c *ruleConfigurations) bindEntry(path string, config *repoconfig.Config) *binding.Scope {
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

func (c *ruleConfigurations) resolve(config *repoconfig.Config, err error) (*repoconfig.Config, error) {
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

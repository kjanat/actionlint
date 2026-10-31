package config

// RunnerLabelOptions supplies repository-specific labels to runner validation
// and execution inference, independently of the diagnostic's selected level.
type RunnerLabelOptions struct {
	// Labels lists additional accepted runner labels or Go path.Match patterns.
	Labels []string `yaml:"labels" jsonschema:"nullable"`
}

// ExpressionReferenceOptions declares available names. Null disables checking;
// an explicit empty list permits no configured names.
type ExpressionReferenceOptions struct {
	// Variables lists configured variable names; null disables checking and [] allows none.
	Variables []string `yaml:"variables" jsonschema:"nullable"`
	// Secrets adds configured secret names to built-in and workflow_call names.
	Secrets []string `yaml:"secrets" jsonschema:"nullable"`
}

// MarshalYAML encodes nil name sets as null and empty name sets as [].
func (o ExpressionReferenceOptions) MarshalYAML() (any, error) {
	value := map[string]any{"variables": nil, "secrets": nil}
	if o.Variables != nil {
		value["variables"] = o.Variables
	}
	if o.Secrets != nil {
		value["secrets"] = o.Secrets
	}
	return value, nil
}

// WorkflowCallOptions sets repository assumptions for reusable workflow checks.
type WorkflowCallOptions struct {
	// DefaultPermissions assumes restricted (default) or permissive repository token permissions.
	DefaultPermissions DefaultPermissionsAssumption `yaml:"default-permissions" jsonschema:"nullable"`
}

func configureRunnerLabels(c *Config, s Setting) {
	if options := new(RunnerLabelOptions); loadOptions(s, options) {
		c.SelfHostedRunner.Labels = options.Labels
	}
}

func configureExpressionReferences(c *Config, s Setting) {
	if options := new(ExpressionReferenceOptions); loadOptions(s, options) {
		c.ConfigVariables, c.ConfigSecrets = options.Variables, options.Secrets
	}
}

func configureWorkflowCall(c *Config, s Setting) {
	if options := new(WorkflowCallOptions); loadOptions(s, options) {
		c.AssumeDefaultPermissions = options.DefaultPermissions
	}
}

// projectSettings keeps legacy Go API fields as internal consumer projections;
// serialized configuration has only the grouped rule vocabulary.
func projectSettings(c *Config) {
	for _, descriptor := range Rules() {
		setting, exists := c.Lint.Rules.Groups()[descriptor.Group].Rule(descriptor.Name)
		if !exists || descriptor.Configure == nil {
			continue
		}
		if (setting.Level() == "" || setting.Level() == "default") && !setting.HasOptions() {
			continue
		}
		if setting.Level() == "" || setting.Level() == "default" {
			setting = setting.WithLevel("off")
			if descriptor.Recommended {
				setting = setting.WithLevel("on")
			}
		}
		descriptor.Configure(c, setting)
		if setting.Level() == "off" {
			switch descriptor.Name {
			case "require-job-timeout":
				c.Policy.RequireJobTimeout = &JobTimeoutPolicy{}
			case "require-permissions":
				c.Policy.RequirePermissions = &PermissionsPolicy{}
			case "disallow-suppressions":
				c.Policy.DisallowSuppressions = &SuppressionsPolicy{}
			}
		}
	}
	if c.Lint.MessageIgnores != nil {
		c.Paths = make(map[string]PathConfig, len(c.Lint.MessageIgnores))
		for path, patterns := range c.Lint.MessageIgnores {
			c.Paths[path] = PathConfig{Ignore: patterns}
		}
	}
}

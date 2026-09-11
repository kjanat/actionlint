package actionlint

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// configOrigins asks the YAML decoder to resolve each mapping, including merge precedence.
func configOrigins(node *yaml.Node, prefix string, origins map[string]ConfigOrigin) error {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var values map[string]yaml.Node
	if err := node.Decode(&values); err != nil {
		return err
	}
	for key, value := range values {
		pointer := prefix + "/" + configPointerPart(key)
		if _, ok := origins[pointer]; !ok {
			continue
		}
		state := "value"
		if value.Tag == "!!null" {
			state = "null"
		}
		origins[pointer] = ConfigOrigin{Source: "config", State: state, Line: value.Line, Column: value.Column}
		if err := configOrigins(&value, pointer, origins); err != nil {
			return err
		}
	}
	return nil
}

func configPointerPart(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }

func effectiveConfig(cfg *Config) map[string]any {
	optionalList := func(values []string) any {
		if values == nil {
			return nil
		}
		return values
	}
	permissions := "restricted"
	if cfg.AssumeDefaultPermissions == DefaultPermissionsAssumptionPermissive {
		permissions = "permissive"
	}
	var timeout any = false
	if policy := cfg.RequiresJobTimeout(); policy.Enabled() {
		bounds := map[string]any{}
		if minimum, ok := policy.MinMinutes(); ok {
			bounds["min-minutes"] = minimum
		}
		if maximum, ok := policy.MaxMinutes(); ok {
			bounds["max-minutes"] = maximum
		}
		timeout = bounds
	}
	var requiredPermissions any = false
	if policy := cfg.RequiresPermissions(); policy.Enabled() {
		requiredPermissions = map[string]any{"scope": policy.Scope()}
	}
	paths := map[string]any{}
	for pattern, path := range cfg.Paths {
		patterns := make([]string, 0, len(path.Ignore))
		for _, p := range path.Ignore {
			patterns = append(patterns, p.String())
		}
		paths[pattern] = map[string]any{"ignore": patterns}
	}
	labels := cfg.SelfHostedRunner.Labels
	if labels == nil {
		labels = []string{}
	}
	required := cfg.RequiredActions()
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"self-hosted-runner":         map[string]any{"labels": labels},
		"config-variables":           optionalList(cfg.ConfigVariables),
		"config-secrets":             optionalList(cfg.ConfigSecrets),
		"assume-default-permissions": permissions,
		"paths":                      paths,
		"policy": map[string]any{
			"require-commit-hash": cfg.RequiresCommitHash(),
			"require-job-timeout": timeout,
			"require-permissions": requiredPermissions,
			"required-actions":    required,
		},
	}
}

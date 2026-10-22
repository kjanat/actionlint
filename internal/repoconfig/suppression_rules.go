package repoconfig

import (
	"fmt"

	"go.yaml.in/yaml/v4"
)

// Rule identity is validated against the caller's catalog after YAML shape decoding.
func validateSuppressionRuleNames(root *yaml.Node, registeredRules []string) error {
	if ignores := configField(root, "ignores"); ignores != nil {
		for _, ignore := range ignores.Content {
			if name := configField(ignore, "rule"); name != nil && !Suppressible(name.Value, registeredRules...) {
				return fmt.Errorf("ignores: unknown suppressible rule %q", name.Value)
			}
		}
	}
	if err := validatePolicyRuleNames(configField(configField(root, "policy"), "disallow-suppressions"), registeredRules); err != nil {
		return err
	}
	if err := validateLintRuleNames(configField(root, "lint"), registeredRules); err != nil {
		return err
	}
	if overrides := configField(root, "overrides"); overrides != nil {
		for _, override := range overrides.Content {
			if err := validateLintRuleNames(configField(override, "lint"), registeredRules); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateLintRuleNames(lint *yaml.Node, registeredRules []string) error {
	rules := configField(lint, "rules")
	if disabled := configField(rules, "disable"); disabled != nil {
		for _, name := range disabled.Content {
			if name.Kind != yaml.ScalarNode || name.ShortTag() != "!!str" || name.Value == "" || !Suppressible(name.Value, registeredRules...) {
				return fmt.Errorf("yaml: unknown diagnostic rule %q in lint.rules.disable", name.Value)
			}
		}
	}
	policy := configField(configField(rules, "policy"), "disallow-suppressions")
	return validatePolicyRuleNames(configField(policy, "options"), registeredRules)
}

func validatePolicyRuleNames(policy *yaml.Node, registeredRules []string) error {
	if rules := configField(policy, "rules"); rules != nil {
		for _, name := range rules.Content {
			if !Suppressible(name.Value, registeredRules...) {
				return suppressionConfigError(name, fmt.Sprintf("unknown suppressible rule %q", name.Value))
			}
		}
	}
	return nil
}

func configField(node *yaml.Node, key string) *yaml.Node {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return nil
}

package config

import (
	"errors"
	"fmt"

	"github.com/kjanat/go-yaml/v4"
)

func removeField(node *yaml.Node, name string) {
	for i := 0; node != nil && i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func migrateVariableNames(node *yaml.Node) (*yaml.Node, error) {
	if node.ShortTag() == "!!null" {
		return node, nil
	}
	if _, err := repositoryNames(node); err != nil {
		return nil, err
	}
	variables := mapping()
	variables.Line, variables.Column = node.Line, node.Column
	for _, name := range node.Content {
		variables.Content = append(variables.Content, name, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
	}
	return variables, nil
}

func migratePermissions(node *yaml.Node, key string) (*yaml.Node, error) {
	if node.ShortTag() == "!!null" {
		return node, nil
	}
	invalid := func() (*yaml.Node, error) {
		return nil, fmt.Errorf("yaml: %q must be %q or %q but got %q at line:%d,col:%d", key, "restricted", "permissive", node.Value, node.Line, node.Column)
	}
	if node.ShortTag() != "!!str" {
		return invalid()
	}
	value := *node
	switch node.Value {
	case "restricted":
		value.Value = "read"
	case "permissive":
		value.Value = "write"
	default:
		return invalid()
	}
	return &value, nil
}

// migrateRepositoryFacts moves old knowledge settings without changing a rule's level.
func migrateRepositoryFacts(root *yaml.Node) (*yaml.Node, bool, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return root, false, nil
	}
	next := cloneMapping(root)
	repo := cloneMapping(field(root, "repository"))
	changed := false
	install := func(name string, value *yaml.Node, origin string) error {
		if repo.Kind != yaml.MappingNode {
			return fmt.Errorf("cannot migrate %s alongside a repository reset", origin)
		}
		if previous := field(repo, name); previous != nil {
			merged, err := mergeMigration(previous, value, "repository."+name, origin)
			if err != nil {
				return err
			}
			value = merged
		}
		setField(repo, name, value)
		changed = true
		return nil
	}
	for _, legacy := range []struct{ key, name string }{
		{"self-hosted-runner", "runners"}, {"config-variables", "variables"},
		{"config-secrets", "secrets"}, {"assume-default-permissions", "default-workflow-permissions"},
	} {
		value := field(root, legacy.key)
		if value == nil {
			continue
		}
		var err error
		switch legacy.key {
		case "self-hosted-runner":
			if value.ShortTag() != "!!null" {
				if value.Kind != yaml.MappingNode {
					return nil, false, errors.New("self-hosted-runner must be a mapping")
				}
				value = field(value, "labels")
			}
		case "config-variables":
			value, err = migrateVariableNames(value)
		case "assume-default-permissions":
			value, err = migratePermissions(value, legacy.key)
		}
		if err != nil {
			return nil, false, err
		}
		if value != nil {
			if err := install(legacy.name, value, legacy.key); err != nil {
				return nil, false, err
			}
		}
		removeField(next, legacy.key)
		changed = true
	}
	if lint := field(root, "lint"); lint != nil && lint.Kind == yaml.MappingNode {
		lint = cloneMapping(lint)
		rules := cloneMapping(field(lint, "rules"))
		type optionMigration struct{ old, next string }
		for _, migration := range []struct {
			name    string
			options []optionMigration
		}{
			{"runner-label", []optionMigration{{"labels", "runners"}}},
			{"expression-reference", []optionMigration{{"variables", "variables"}, {"secrets", "secrets"}}},
			{"workflow-call", []optionMigration{{"default-permissions", "default-workflow-permissions"}}},
		} {
			name := migration.name
			descriptor, _ := FindRule(name)
			group := cloneMapping(field(rules, descriptor.Group))
			setting := field(group, name)
			if setting == nil || setting.Kind != yaml.MappingNode {
				continue
			}
			options := field(setting, "options")
			if options == nil || options.Kind != yaml.MappingNode {
				continue
			}
			setting, options = cloneMapping(setting), cloneMapping(options)
			for _, option := range migration.options {
				value := field(options, option.old)
				if value == nil {
					continue
				}
				var err error
				if option.old == "variables" {
					value, err = migrateVariableNames(value)
				}
				if option.old == "default-permissions" {
					value, err = migratePermissions(value, "default-permissions")
				}
				if err != nil {
					return nil, false, err
				}
				if err := install(option.next, value, "lint.rules."+descriptor.Group+"."+name+".options."+option.old); err != nil {
					return nil, false, err
				}
				removeField(options, option.old)
			}
			if len(options.Content) == 0 {
				removeField(setting, "options")
			} else {
				setField(setting, "options", options)
			}
			setField(group, name, setting)
			setField(rules, descriptor.Group, group)
		}
		if rules.Kind == yaml.MappingNode && len(rules.Content) != 0 {
			setField(lint, "rules", rules)
		}
		setField(next, "lint", lint)
	}
	if changed && len(repo.Content) != 0 {
		setField(next, "repository", repo)
	}
	if overrides := field(root, "overrides"); overrides != nil && overrides.Kind == yaml.SequenceNode {
		entries := *overrides
		entries.Content = append([]*yaml.Node{}, overrides.Content...)
		for i, entry := range entries.Content {
			migrated, converted, err := migrateRepositoryFacts(entry)
			if err != nil {
				return nil, false, err
			}
			entries.Content[i], changed = migrated, changed || converted
		}
		setField(next, "overrides", &entries)
	}
	return next, changed, nil
}

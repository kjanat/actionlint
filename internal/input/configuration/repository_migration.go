package config

import (
	"errors"
	"fmt"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
)

func migrateVariableNames(node any) (any, error) {
	if isNull(node) {
		return node, nil
	}
	entries, ok := node.([]any)
	if !ok {
		return nil, errors.New("repository names must be a sequence")
	}
	names := make([]string, len(entries))
	variables := mapping()
	for i, item := range entries {
		name, ok := item.(string)
		if !ok {
			return nil, binding.At(strconv.Itoa(i), errors.New("repository names must be strings"))
		}
		names[i] = name
		variables[name] = nil
	}
	if _, err := repository.NewNames(names...); err != nil {
		return nil, err
	}
	return variables, nil
}

func migratePermissions(node any, key string) (any, error) {
	if isNull(node) {
		return node, nil
	}
	switch text(node) {
	case "restricted":
		return "read", nil
	case "permissive":
		return "write", nil
	default:
		return nil, fmt.Errorf("%q must be %q or %q but got %q", key, "restricted", "permissive", node)
	}
}

// migrateRepositoryFacts moves old knowledge settings without changing a rule's level.
func (ruleSet RuleSet) migrateRepositoryFacts(root any) (any, bool, error) {
	if !isMapping(root) {
		return root, false, nil
	}
	next := cloneMapping(root)
	repo := cloneMapping(field(root, "repository"))
	changed := false
	install := func(name string, value any, origin string) error {
		if !isMapping(repo) {
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
		{"self-hosted-runner", "runners"},
		{"config-variables", "variables"},
		{"config-secrets", "secrets"},
		{"assume-default-permissions", "default-workflow-permissions"},
	} {
		value := field(root, legacy.key)
		if value == nil {
			continue
		}
		var err error
		switch legacy.key {
		case "self-hosted-runner":
			if !isNull(value) {
				if !isMapping(value) {
					return nil, false, binding.At(legacy.key, errors.New("self-hosted-runner must be a mapping"))
				}
				value = field(value, "labels")
			}
		case "config-variables":
			value, err = migrateVariableNames(value)
		case "assume-default-permissions":
			value, err = migratePermissions(value, legacy.key)
		}
		if err != nil {
			return nil, false, binding.At(legacy.key, err)
		}
		if value != nil {
			if err := install(legacy.name, value, legacy.key); err != nil {
				return nil, false, binding.At(legacy.key, err)
			}
		}
		removeField(next, legacy.key)
		changed = true
	}
	if lint := field(root, "lint"); isMapping(lint) {
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
			descriptor, _ := ruleSet.FindRule(name)
			group := cloneMapping(field(rules, descriptor.Group))
			setting := field(group, name)
			if !isMapping(setting) {
				continue
			}
			options := field(setting, "options")
			if !isMapping(options) {
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
					return nil, false, binding.At("lint", binding.At("rules", binding.At(descriptor.Group, binding.At(name, binding.At("options", binding.At(option.old, err))))))
				}
				if err := install(option.next, value, "lint.rules."+descriptor.Group+"."+name+".options."+option.old); err != nil {
					return nil, false, binding.At("lint", binding.At("rules", binding.At(descriptor.Group, binding.At(name, binding.At("options", binding.At(option.old, err))))))
				}
				removeField(options, option.old)
			}
			if len(members(options)) == 0 {
				removeField(setting, "options")
			} else {
				setField(setting, "options", options)
			}
			setField(group, name, setting)
			setField(rules, descriptor.Group, group)
		}
		if isMapping(rules) && len(members(rules)) != 0 {
			setField(lint, "rules", rules)
		}
		setField(next, "lint", lint)
	}
	if changed && len(members(repo)) != 0 {
		setField(next, "repository", repo)
	}
	if overrides, ok := field(root, "overrides").([]any); ok {
		entries := append([]any(nil), overrides...)
		convertedEntries := false
		for i, entry := range entries {
			migrated, converted, err := ruleSet.migrateRepositoryFacts(entry)
			if err != nil {
				return nil, false, err
			}
			if converted {
				entries[i] = migrated
				convertedEntries = true
			}
		}
		if convertedEntries {
			setField(next, "overrides", entries)
			changed = true
		}
	}

	if !changed {
		return root, false, nil
	}
	return next, changed, nil
}

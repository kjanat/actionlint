package configuration

import (
	"errors"
	"fmt"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"

	"actionlint.kjanat.dev/v2/internal/model/repository"
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
			return nil, configdecode.At(strconv.Itoa(i), errors.New("repository names must be strings"))
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

func migrateRepositoryValue(fact string, value any, key string) (any, error) {
	switch fact {
	case "variables":
		return migrateVariableNames(value)
	case "default-workflow-permissions":
		return migratePermissions(value, key)
	default:
		return value, nil
	}
}

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
		if legacy.key == "self-hosted-runner" {
			if !isNull(value) {
				if !isMapping(value) {
					return nil, false, configdecode.At(legacy.key, errors.New("self-hosted-runner must be a mapping"))
				}
				value = field(value, "labels")
			}
		} else {
			value, err = migrateRepositoryValue(legacy.name, value, legacy.key)
		}
		if err != nil {
			return nil, false, configdecode.At(legacy.key, err)
		}
		if value != nil {
			if err := install(legacy.name, value, legacy.key); err != nil {
				return nil, false, configdecode.At(legacy.key, err)
			}
		}
		removeField(next, legacy.key)
		changed = true
	}
	if lint := field(root, "lint"); isMapping(lint) {
		lint = cloneMapping(lint)
		rules := cloneMapping(field(lint, "rules"))
		for _, descriptor := range ruleSet.Rules() {
			if len(descriptor.MovedOptions) == 0 {
				continue
			}
			name := descriptor.Name
			group := cloneMapping(field(rules, descriptor.Category))
			setting := field(group, name)
			if !isMapping(setting) {
				continue
			}
			options := field(setting, "options")
			if !isMapping(options) {
				continue
			}
			setting, options = cloneMapping(setting), cloneMapping(options)
			for _, option := range descriptor.MovedOptions {
				value := field(options, option.Option)
				if value == nil {
					continue
				}
				value, err := migrateRepositoryValue(option.Repository, value, option.Option)
				if err != nil {
					return nil, false, configdecode.At("lint", configdecode.At("rules", configdecode.At(descriptor.Category, configdecode.At(name, configdecode.At("options", configdecode.At(option.Option, err))))))
				}
				if err := install(option.Repository, value, "lint.rules."+descriptor.Category+"."+name+".options."+option.Option); err != nil {
					return nil, false, configdecode.At("lint", configdecode.At("rules", configdecode.At(descriptor.Category, configdecode.At(name, configdecode.At("options", configdecode.At(option.Option, err))))))
				}
				removeField(options, option.Option)
			}
			if len(members(options)) == 0 {
				removeField(setting, "options")
			} else {
				setField(setting, "options", options)
			}
			setField(group, name, setting)
			setField(rules, descriptor.Category, group)
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

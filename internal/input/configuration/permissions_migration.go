package config

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func migratedPermissionsName(name string) string {
	switch name {
	case "require-permissions", "explicit-permissions", "missing-workflow-permissions":
		return "missing-permissions"
	default:
		return name
	}
}

func migratePermissionsSetting(name string, selected any) (any, error) {
	setting := cloneMapping(selected)
	if !isMapping(selected) {
		setting = mapping()
		setField(setting, "level", selected)
	}
	if name == "missing-workflow-permissions" {
		level := field(setting, "level")
		if level == nil || text(level) == "on" {
			setField(setting, "level", textValue("warn"))
		}
		return setting, nil
	}
	scope := "workflow"
	if options := field(setting, "options"); options != nil && !isNull(options) {
		var legacy PermissionsPolicy
		if err := decodeValue(options, &legacy); err != nil {
			return nil, err
		}
		scope = legacy.Scope()
	}
	if scope == "job" {
		scope = "every-job"
	}
	options := mapping()
	setField(options, "require-at", textValue(scope))
	setField(setting, "options", options)
	return setting, nil
}

func (ruleSet RuleSet) migratePermissionsRule(root any) (any, bool, error) {
	if !isMapping(root) {
		return root, false, nil
	}
	next := cloneMapping(root)
	lint := cloneMapping(field(root, "lint"))
	rules := cloneMapping(field(lint, "rules"))
	changed := false
	install := func(name string, setting any) error {
		if !isMapping(lint) || !isMapping(rules) {
			return errors.New("cannot migrate permission requirements alongside non-object lint or lint.rules")
		}

		group, err := migrationGroup(field(rules, "security"))
		if err != nil {
			return err
		}
		if previous := field(group, "missing-permissions"); previous != nil {
			setting, err = mergeMigration(previous, setting, "lint.rules.security.missing-permissions", name)
			if err != nil {
				return fmt.Errorf("cannot combine permission requirements automatically; select missing-permissions and require-at explicitly: %w", err)
			}
		}
		setField(group, "missing-permissions", setting)
		setField(rules, "security", group)
		return nil
	}
	for category, original := range members(rules) {
		if !isMapping(original) {
			continue
		}
		group := cloneMapping(original)
		for name, originalSetting := range members(original) {
			if migratedPermissionsName(name) == name {
				continue
			}
			setting, err := migratePermissionsSetting(name, originalSetting)
			if err != nil {
				return nil, false, binding.At("lint", binding.At("rules", binding.At(category, binding.At(name, binding.At("options", err)))))
			}
			removeField(group, name)
			setField(rules, category, group)
			if err := install(name, setting); err != nil {
				return nil, false, err
			}
			if category == "security" {
				group = cloneMapping(field(rules, "security"))
			}
			changed = true
		}
		setField(rules, category, group)
	}
	if policy := field(root, "policy"); policy != nil && isMapping(policy) {
		if selected := field(policy, "require-permissions"); selected != nil {
			if !isMapping(selected) && !isBool(selected) && !isNull(selected) {
				return nil, false, binding.At("policy", binding.At("require-permissions", errors.New("policy.require-permissions must be a boolean or a scope mapping")))
			}
			level := "default"
			if isBool(selected) {
				level = "off"
			}
			if isMapping(selected) || selected == true {
				level = "on"
			}
			var setting any = map[string]any{"level": level, "options": nil}
			if !isNull(selected) {
				if isMapping(selected) {
					setField(setting, "options", selected)
				}
				var err error
				setting, err = migratePermissionsSetting("require-permissions", setting)
				if err != nil {
					return nil, false, binding.At("policy", binding.At("require-permissions", err))
				}
			}

			if err := install("policy.require-permissions", setting); err != nil {
				return nil, false, err
			}
			policy = cloneMapping(policy)
			removeField(policy, "require-permissions")
			if len(members(policy)) == 0 {
				removeField(next, "policy")
			} else {
				setField(next, "policy", policy)
			}
			changed = true
		}
	}
	if overrides := field(root, "overrides"); overrides != nil && isList(overrides) {
		entries := append([]any{}, elements(overrides)...)
		for i, entry := range elements(overrides) {
			updated, migrated, err := ruleSet.migratePermissionsRule(entry)
			if err != nil {
				return nil, false, err
			}
			entries[i] = updated
			changed = changed || migrated
		}
		if changed {
			setField(next, "overrides", entries)
		}
	}
	if !changed {
		return root, false, nil
	}
	setField(lint, "rules", rules)
	setField(next, "lint", lint)
	return next, true, nil
}

package config

import (
	"errors"
	"fmt"
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

// ErrRuleMetadataRequired means a category migration needs plugin descriptors.
// Ordinary analysis may defer this conversion until plugins are registered.
var ErrRuleMetadataRequired = errors.New("registered rule metadata required")

// migrateRuleAliases changes identifiers before inheritance, without flattening
// group defaults. The latter must be merged before their effect can be resolved.
func (ruleSet RuleSet) migrateRuleAliases(root any) (any, bool, error) {
	if !isMapping(root) {
		return root, false, nil
	}
	next := cloneMapping(root)
	changed := false
	lint := cloneMapping(field(root, "lint"))
	rules := cloneMapping(field(lint, "rules"))
	for category, original := range members(rules) {
		if original == nil {
			original = explicitNull{}
		}
		if category == "disable" {
			updated, renamed := ruleSet.renameSelectorList(original)
			setField(rules, category, updated)
			changed = changed || renamed
			continue
		}
		if !rule.ValidCategory(category) && !slices.Contains([]string{"style", "policy", "external", "plugins", "nursery"}, category) || !isMapping(original) {
			continue
		}
		group := cloneMapping(original)
		for name, setting := range members(original) {
			if setting == nil {
				setting = explicitNull{}
			}
			canonical := ruleSet.ResolveAlias(migratedPermissionsName(name))
			if canonical == "forbidden-suppression" {
				if options := field(setting, "options"); isMapping(options) {
					if list := field(options, "rules"); list != nil {
						updated, renamed := ruleSet.renameSelectorList(list)
						if renamed {
							options = cloneMapping(options)
							setField(options, "rules", updated)
							setting = cloneMapping(setting)
							setField(setting, "options", options)
							setField(group, name, setting)
							changed = true
						}
					}
				}
			}
			if canonical == name {
				continue
			}
			if previous := field(group, canonical); previous != nil {
				merged, err := mergeMigration(previous, setting, "lint.rules."+category+"."+canonical, "lint.rules."+category+"."+name)
				if err != nil {
					return nil, false, err
				}
				setting = merged
			}
			removeField(group, name)
			setField(group, canonical, setting)
			changed = true
		}
		setField(rules, category, group)
	}
	if changed {
		setField(lint, "rules", rules)
		setField(next, "lint", lint)
	}
	if ignores, ok := field(root, "ignores").([]any); ok {
		entries := slices.Clone(ignores)
		for i, entry := range entries {
			name, ok := field(entry, "rule").(string)
			if ok {
				updated := ruleSet.ResolveAlias(migratedPermissionsName(name))
				if updated != name {
					entry = cloneMapping(entry)
					setField(entry, "rule", updated)
					entries[i] = entry
					changed = true
				}
			}
		}
		setField(next, "ignores", entries)
	}
	if overrides, ok := field(root, "overrides").([]any); ok {
		entries := slices.Clone(overrides)
		for i, entry := range entries {
			updated, renamed, err := ruleSet.migrateRuleAliases(entry)
			if err != nil {
				return nil, false, err
			}
			entries[i] = updated
			changed = changed || renamed
		}
		setField(next, "overrides", entries)
	}

	if !changed {
		// Origins are keyed by node identity. A no-op migration must not
		// replace nodes carrying overlay or reset origins.
		return root, false, nil
	}
	return next, changed, nil
}

func (ruleSet RuleSet) renameSelectorList(node any) (any, bool) {
	entries, ok := node.([]any)
	if !ok {
		return node, false
	}
	next := slices.Clone(entries)
	changed := false
	for i, item := range entries {
		name, ok := item.(string)
		if ok {
			updated := ruleSet.ResolveAlias(migratedPermissionsName(name))
			if updated != name {
				next[i] = updated
				changed = true
			}
		}
	}
	return next, changed
}

// migrateRuleCategories rewrites an explicit migration candidate. Runtime
// normalization keeps legacy controls until the complete registration is known.
func (ruleSet RuleSet) migrateRuleCategories(root any, registrations []rule.Descriptor) (any, bool, error) {
	if !isMapping(root) {
		return root, false, nil
	}
	// Binding validates duplicate IDs, category contracts and replacement rules.
	if _, err := ruleSet.ResolveRules(Presets{}, &Config{}, registrations...); err != nil {
		return nil, false, err
	}
	descriptors := ruleSet.Rules()
	registered := map[string]bool{}
	for _, descriptor := range registrations {
		registered[descriptor.Name] = true
		if index := slices.IndexFunc(descriptors, func(d Rule) bool { return d.Name == descriptor.Name }); index >= 0 {
			descriptors[index].Descriptor = descriptor
		} else {
			descriptors = append(descriptors, Rule{Descriptor: descriptor, Group: descriptor.Category})
		}
	}
	lookup := func(name string) (Rule, bool) {
		for _, descriptor := range descriptors {
			if descriptor.Name == name {
				return descriptor, true
			}
		}
		return Rule{}, false
	}
	next := cloneMapping(root)
	lint := cloneMapping(field(root, "lint"))
	rules := cloneMapping(field(lint, "rules"))
	changed := false
	originalRules := cloneMapping(rules)
	explicitSelections := map[string]bool{}
	install := func(category, name string, setting any, origin string) error {
		group, err := migrationGroup(field(rules, category))
		if err != nil {
			return err
		}
		if previous := field(field(originalRules, category), name); previous != nil {
			setting, err = mergeMigration(previous, setting, "lint.rules."+category+"."+name, origin)
			if err != nil {
				return err
			}
		}
		setField(group, name, setting)
		setField(rules, category, group)
		return nil
	}
	for _, category := range append([]string{"style", "policy", "external", "nursery", "plugins"}, lintconfig.Categories()...) {
		node := field(rules, category)
		if node == nil {
			continue
		}
		var group Group
		if err := decodeValue(node, &group); err != nil {
			return nil, false, err
		}
		legacy := !rule.ValidCategory(category)
		if (category == "plugins" || category == "nursery") && len(registrations) == 0 && (group.Level() != "" || group.Preset() != "") {
			return nil, false, fmt.Errorf("cannot migrate lint.rules.%s: %w", category, ErrRuleMetadataRequired)
		}
		selections := map[string]any{}
		if legacy && (group.Level() != "" || group.Preset() != "" || isNull(node)) {
			for _, descriptor := range descriptors {
				member := ruleSet.LegacyGroup(descriptor.Name) == category
				switch category {
				case "plugins":
					member = registered[descriptor.Name]
				case "nursery":
					member = descriptor.Experimental()
				}
				if !member {
					continue
				}
				level := presetLevel(group.Preset(), descriptor, "default")
				if category == "nursery" && group.Preset() == "all" {
					level = "on"
				}
				if group.Level() != "" && group.Level() != "default" {
					level = group.Level()
				}
				if isNull(node) {
					selections[descriptor.Name] = node
				} else {
					selections[descriptor.Name] = string(level)
				}
			}
		}
		if isMapping(node) {
			for name, setting := range members(node) {
				if setting == nil {
					setting = explicitNull{}
				}
				if name == "level" || name == "preset" {
					continue
				}
				descriptor, known := lookup(name)
				if !known {
					if legacy {
						return nil, false, fmt.Errorf("cannot migrate lint.rules.%s.%s: %w", category, name, ErrRuleMetadataRequired)
					}
					continue
				}
				if !legacy && descriptor.Category == category {
					continue
				}
				if legacy && category != "plugins" && category != "nursery" && ruleSet.LegacyGroup(name) != category {
					return nil, false, fmt.Errorf("rule %q belongs in lint.rules.%s, not lint.rules.%s", name, descriptor.Category, category)
				}
				if baseline := selections[name]; baseline != nil {
					explicit, _ := group.Rule(name)
					if explicit.Level() == "" || explicit.Level() == "default" {
						if isMapping(setting) {
							setting = cloneMapping(setting)
							setField(setting, "level", baseline)
						} else {
							setting = baseline
						}
					}
				}
				selections[name] = setting
			}
		}
		if legacy {
			removeField(rules, category)
			changed = true
		}
		for _, name := range sortedSelectionNames(selections) {
			descriptor, _ := lookup(name)
			isExplicit := field(node, name) != nil
			if !isExplicit {
				if explicitSelections[name] {
					continue
				}
				var target Group
				if destination := field(originalRules, descriptor.Category); destination != nil {
					if err := decodeValue(destination, &target); err != nil {
						return nil, false, err
					}
				}
				// A current category's defaults and named exceptions take
				// precedence over an old group's expanded baseline.
				if _, exists := target.Rule(name); exists || target.Preset() != "" || target.Level() != "" && target.Level() != "default" {
					continue
				}
			}
			explicitSelections[name] = isExplicit
			if !legacy {
				old := cloneMapping(field(rules, category))
				removeField(old, name)
				setField(rules, category, old)
			}
			if err := install(descriptor.Category, name, selections[name], "lint.rules."+category+"."+name); err != nil {
				return nil, false, err
			}
			changed = true
		}
	}
	if changed {
		setField(lint, "rules", rules)
		setField(next, "lint", lint)
	}
	if overrides, ok := field(root, "overrides").([]any); ok {
		entries := slices.Clone(overrides)
		for i, entry := range entries {
			updated, migrated, err := ruleSet.migrateRuleCategories(entry, registrations)
			if err != nil {
				return nil, false, err
			}
			entries[i] = updated
			changed = changed || migrated
		}
		setField(next, "overrides", entries)
	}

	return next, changed, nil
}

func sortedSelectionNames(selections map[string]any) []string {
	names := make([]string, 0, len(selections))
	for name := range selections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func migrationGroup(node any) (any, error) {
	if node == nil || isMapping(node) {
		return cloneMapping(node), nil
	}
	var group Group
	if err := decodeValue(node, &group); err != nil {
		return nil, err
	}
	next := mapping()
	if group.Level() != "" {
		setField(next, "level", string(group.Level()))
	}
	if group.Preset() != "" {
		setField(next, "preset", string(group.Preset()))
	}
	return next, nil
}

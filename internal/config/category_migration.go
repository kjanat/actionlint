package config

import (
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/rule"
	"github.com/kjanat/go-yaml/v4"
)

// ErrRuleMetadataRequired means a category migration needs plugin descriptors.
// Ordinary analysis may defer this conversion until plugins are registered.
var ErrRuleMetadataRequired = errors.New("registered rule metadata required")

// migrateRuleAliases changes identifiers before inheritance, without flattening
// group defaults. The latter must be merged before their effect can be resolved.
func migrateRuleAliases(root *yaml.Node) (*yaml.Node, bool, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return root, false, nil
	}
	next := cloneMapping(root)
	changed := false
	lint := cloneMapping(field(root, "lint"))
	rules := cloneMapping(field(lint, "rules"))
	for i := 0; rules.Kind == yaml.MappingNode && i+1 < len(rules.Content); i += 2 {
		category, original := rules.Content[i].Value, rules.Content[i+1]
		if category == "disable" {
			updated, renamed := renameSelectorList(original)
			rules.Content[i+1] = updated
			changed = changed || renamed
			continue
		}
		if !rule.ValidCategory(category) && !slices.Contains([]string{"style", "policy", "external", "plugins", "nursery"}, category) || original.Kind != yaml.MappingNode {
			continue
		}
		group := cloneMapping(original)
		for j := 0; j+1 < len(original.Content); j += 2 {
			name, setting := original.Content[j].Value, original.Content[j+1]
			canonical := rule.CanonicalName(name)
			if canonical == "forbidden-suppression" {
				if options := field(setting, "options"); options != nil && options.Kind == yaml.MappingNode {
					if list := field(options, "rules"); list != nil {
						updated, renamed := renameSelectorList(list)
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
		rules.Content[i+1] = group
	}
	if changed {
		setField(lint, "rules", rules)
		setField(next, "lint", lint)
	}
	if ignores := field(root, "ignores"); ignores != nil && ignores.Kind == yaml.SequenceNode {
		entries := *ignores
		entries.Content = slices.Clone(ignores.Content)
		for i, entry := range entries.Content {
			if name := field(entry, "rule"); name != nil && name.Tag == "!!str" && rule.CanonicalName(name.Value) != name.Value {
				updated := *name
				updated.Value = rule.CanonicalName(name.Value)
				entry = cloneMapping(entry)
				setField(entry, "rule", &updated)
				entries.Content[i] = entry
				changed = true
			}
		}
		setField(next, "ignores", &entries)
	}
	if overrides := field(root, "overrides"); overrides != nil && overrides.Kind == yaml.SequenceNode {
		entries := *overrides
		entries.Content = slices.Clone(overrides.Content)
		for i, entry := range entries.Content {
			updated, renamed, err := migrateRuleAliases(entry)
			if err != nil {
				return nil, false, err
			}
			entries.Content[i] = updated
			changed = changed || renamed
		}
		setField(next, "overrides", &entries)
	}
	return next, changed, nil
}

func renameSelectorList(node *yaml.Node) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return node, false
	}
	next := *node
	next.Content = slices.Clone(node.Content)
	changed := false
	for i, name := range node.Content {
		if name.Tag == "!!str" && rule.CanonicalName(name.Value) != name.Value {
			updated := *name
			updated.Value = rule.CanonicalName(name.Value)
			next.Content[i] = &updated
			changed = true
		}
	}
	return &next, changed
}

// migrateRuleCategories rewrites an explicit migration candidate. Runtime
// normalization keeps legacy controls until the complete registration is known.
func migrateRuleCategories(root *yaml.Node, registrations []rule.Descriptor) (*yaml.Node, bool, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return root, false, nil
	}
	// Binding validates duplicate IDs, category contracts and replacement rules.
	if _, err := ResolveRules(Presets{}, &Config{}, registrations...); err != nil {
		return nil, false, err
	}
	descriptors := Rules()
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
	install := func(category, name string, setting *yaml.Node, origin string) error {
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
	for _, category := range []string{"style", "policy", "external", "nursery", "plugins", "correctness", "suspicious", "security", "conventions"} {
		node := field(rules, category)
		if node == nil {
			continue
		}
		var group Group
		if err := node.Decode(&group); err != nil {
			return nil, false, err
		}
		legacy := !rule.ValidCategory(category)
		if (category == "plugins" || category == "nursery") && len(registrations) == 0 && (group.Level() != "" || group.Preset() != "") {
			return nil, false, fmt.Errorf("cannot migrate lint.rules.%s: %w", category, ErrRuleMetadataRequired)
		}
		selections := map[string]*yaml.Node{}
		if legacy && (group.Level() != "" || group.Preset() != "" || node.ShortTag() == "!!null") {
			for _, descriptor := range descriptors {
				member := LegacyGroup(descriptor.Name) == category
				if category == "plugins" {
					member = registered[descriptor.Name]
				} else if category == "nursery" {
					member = descriptor.Nursery()
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
				if node.ShortTag() == "!!null" {
					selections[descriptor.Name] = node
				} else {
					selections[descriptor.Name] = textNode(string(level))
				}
			}
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				name, setting := node.Content[i].Value, node.Content[i+1]
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
				if category != "plugins" && category != "nursery" && LegacyGroup(name) != category {
					return nil, false, fmt.Errorf("rule %q belongs in lint.rules.%s, not lint.rules.%s", name, descriptor.Category, category)
				}
				if baseline := selections[name]; baseline != nil {
					explicit, _ := group.Rule(name)
					if explicit.Level() == "" || explicit.Level() == "default" {
						if setting.Kind == yaml.MappingNode {
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
					if err := destination.Decode(&target); err != nil {
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
	if overrides := field(root, "overrides"); overrides != nil && overrides.Kind == yaml.SequenceNode {
		entries := *overrides
		entries.Content = slices.Clone(overrides.Content)
		for i, entry := range entries.Content {
			updated, migrated, err := migrateRuleCategories(entry, registrations)
			if err != nil {
				return nil, false, err
			}
			entries.Content[i] = updated
			changed = changed || migrated
		}
		setField(next, "overrides", &entries)
	}
	return next, changed, nil
}

func sortedSelectionNames(selections map[string]*yaml.Node) []string {
	names := make([]string, 0, len(selections))
	for name := range selections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func migrationGroup(node *yaml.Node) (*yaml.Node, error) {
	if node == nil || node.Kind == yaml.MappingNode {
		return cloneMapping(node), nil
	}
	var group Group
	if err := node.Decode(&group); err != nil {
		return nil, err
	}
	next := mapping()
	if group.Level() != "" {
		setField(next, "level", textNode(string(group.Level())))
	}
	if group.Preset() != "" {
		setField(next, "preset", textNode(string(group.Preset())))
	}
	return next, nil
}

package config

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

var legacyKeys = []string{"policy", "self-hosted-runner", "config-variables", "config-secrets", "assume-default-permissions", "paths"}

// NormalizeForMerge expands and canonicalizes one document before inheritance
// merging. Plugin-rule binding is deferred until providers are installed.
// Callers must validate the resulting configuration before analysis.
func (ruleSet RuleSet) NormalizeForMerge(node any) (any, error) {
	canonical, _, err := ruleSet.migrateNode(normalizeToolSwitch(node))
	return canonical, err
}

// Migration contains converted configuration in the input's format.
type Migration struct {
	Content []byte   `json:"content"`
	Changed bool     `json:"changed"`
	Notes   []string `json:"notes,omitempty"`
}

// MigrateDocument converts legacy syntax without reading or writing files.
// Explicit migrated policy levels now survive presets and --strict.
func (ruleSet RuleSet) MigrateDocument(path string, content []byte, registrations ...rule.Descriptor) (Migration, error) {
	parsed, err := document.Parse(path, content)
	if err != nil {
		return Migration{}, err
	}
	root := parsed.Value
	for _, warning := range configWarnings(root, nil) {
		if strings.Contains(warning.Message, `"self-hosted-runner.`) || strings.Contains(warning.Message, `"paths[`) {
			return Migration{}, fmt.Errorf("cannot migrate without dropping an unknown setting: %s", warning.Message)
		}
	}
	next, changed, err := ruleSet.migrateNode(root)
	if err != nil {
		return Migration{}, err
	}
	next, categoriesChanged, err := ruleSet.migrateRuleCategories(next, registrations)
	if err != nil {
		return Migration{}, err
	}
	changed = changed || categoriesChanged
	result := Migration{Content: bytes.Clone(content), Changed: changed}
	if !changed {
		return result, nil
	}
	if field(root, "policy") != nil {
		result.Notes = append(result.Notes, "Migrated policy levels are explicit: presets and --strict no longer override explicit disables.")
	}
	if legacyExpressionControls(root) {
		result.Notes = append(result.Notes, "Expression diagnostics have independent rule IDs in v2. Existing expression levels, disable entries and suppressions select only expression; review the split-rule migration in docs/config-migration.md. Workflow comments are not rewritten.")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		result.Content, err = migrateTOML(content, next)
	case ".json", ".jsonc":
		result.Content, err = document.Marshal(path, next)
	default:
		relocated := ruleSet.migrationInputs(root, documentInputs(parsed))
		locations := map[string]document.Location{}
		for pointer, origin := range relocated {
			line, column := origin.KeyLine, origin.KeyColumn
			if line == 0 {
				line, column = origin.Line, origin.Column
			}
			locations[pointer] = document.Location{File: origin.File, Line: line, Column: column}
		}
		result.Content, err = document.RewriteYAML(content, next, locations)
	}
	if err != nil {
		return Migration{}, err
	}

	if !bytes.HasSuffix(result.Content, []byte("\n")) {
		result.Content = append(result.Content, '\n')
	}
	if !strings.EqualFold(filepath.Ext(path), ".toml") && bytes.Contains(content, []byte("\r\n")) {
		result.Content = bytes.ReplaceAll(result.Content, []byte("\n"), []byte("\r\n"))
	}
	return result, nil
}

// The valid v2 expression selector cannot distinguish old broad intent from a
// deliberate field-value selection. Warn during legacy conversion; do not alias it.
func legacyExpressionControls(root any) bool {
	rules := field(field(root, "lint"), "rules")
	if field(field(rules, "correctness"), "expression") != nil {
		return true
	}
	if disabled := field(rules, "disable"); disabled != nil {
		for _, item := range elements(disabled) {
			if text(item) == "expression" {
				return true
			}
		}
	}
	if ignores := field(root, "ignores"); ignores != nil {
		for _, item := range elements(ignores) {
			if selected := field(item, "rule"); selected != nil && text(selected) == "expression" {
				return true
			}
		}
	}
	if overrides := field(root, "overrides"); overrides != nil {
		return slices.ContainsFunc(elements(overrides), legacyExpressionControls)
	}
	return false
}

func (ruleSet RuleSet) migrateNode(root any) (any, bool, error) {
	updated, permissionsChanged, err := ruleSet.migratePermissionsRule(root)
	if err != nil {
		return nil, false, err
	}
	next, renamed, err := ruleSet.migrateRuleAliases(updated)
	if err != nil {
		return nil, false, err
	}
	next, changed, err := ruleSet.migrateLegacyNode(next)
	return next, permissionsChanged || renamed || changed, err
}

func (ruleSet RuleSet) migrateLegacyNode(root any) (any, bool, error) {
	if !isMapping(root) {
		return root, false, nil
	}
	root, changed, err := ruleSet.migrateRepositoryFacts(root)
	if err != nil {
		return nil, false, err
	}
	for _, key := range legacyKeys {
		changed = changed || field(root, key) != nil
	}
	if !changed {
		return root, false, nil
	}
	next := cloneMapping(root)
	lint := cloneMapping(field(root, "lint"))
	if !isMapping(lint) {
		return nil, false, errors.New("cannot migrate legacy settings alongside non-mapping lint; remove the conflicting lint reset")
	}
	rules := cloneMapping(field(lint, "rules"))
	if !isMapping(rules) {
		return nil, false, errors.New("cannot migrate legacy settings alongside non-mapping lint.rules")
	}
	install := func(name string, setting any, origin string) error {
		descriptor, found := ruleSet.FindRule(name)
		if !found {
			return fmt.Errorf("cannot migrate %s: no rule declaration for %q", origin, name)
		}
		name = descriptor.Name
		group := cloneMapping(field(rules, descriptor.Group))
		if !isMapping(group) {
			// Expand a valid group shorthand without dropping its baseline.
			var decoded Group
			if err := decodeValue(group, &decoded); err != nil {
				return err
			}
			group = mapping()
			if decoded.Level() != "" {
				setField(group, "level", textValue(string(decoded.Level())))
			}
			if decoded.Preset() != "" {
				setField(group, "preset", textValue(string(decoded.Preset())))
			}
		}
		if previous := field(group, name); previous != nil {
			merged, err := mergeMigration(previous, setting, "lint.rules."+descriptor.Group+"."+name, origin)
			if err != nil {
				return err
			}
			setting = merged
		}
		setField(group, name, setting)
		setField(rules, descriptor.Group, group)
		return nil
	}
	for _, key := range legacyKeys {
		value := field(root, key)
		if value == nil {
			continue
		}
		var err error
		switch key {
		case "paths":
			var paths map[string]PathConfig
			if err = decodeValue(value, &paths); err != nil {
				break
			}
			messages := any(mapping())
			if isNull(value) {
				messages = value
			} else {
				for pattern, config := range members(value) {
					ignored := field(config, "ignore")
					if ignored == nil {
						if isMapping(config) {
							// An empty mapping inherits the existing path entry;
							// replacing it with [] would erase its patterns.
							continue
						}
						ignored = []any{}
					}
					setField(messages, pattern, ignored)
				}
			}
			if previous := field(lint, "message-ignores"); previous != nil {
				messages, err = mergeMigration(previous, messages, "lint.message-ignores", "paths")
			}
			setField(lint, "message-ignores", messages)
		case "policy":
			var policy policyWire
			if err = decodeValue(value, &policy); err != nil {
				break
			}
			for name, selected := range members(migratedPolicySettings(value)) {
				if err != nil {
					break
				}
				setting := mapping()
				level := "default"
				setField(setting, "options", explicitNull{})
				switch selected.(type) {
				case bool:
					level = "off"
					var enabled bool
					if err = decodeValue(selected, &enabled); err != nil {
						break
					}
					if enabled {
						level = "on"
					}
				case map[string]any:
					level = "on"
					setField(setting, "options", selected)
				}
				levelNode := textValue(level)
				if isNull(selected) {
					levelNode = selected
					setField(setting, "options", selected)
				}
				setField(setting, "level", levelNode)
				err = install(name, setting, "policy."+name)
			}
		}
		if err != nil {
			return nil, false, binding.At(key, err)
		}
		removeField(next, key)
	}
	if len(members(rules)) != 0 {
		setField(lint, "rules", rules)
	}
	if len(members(lint)) != 0 {
		setField(next, "lint", lint)
	}
	return next, true, nil
}

func migratedPolicySettings(value any) any {
	if value == nil || !isNull(value) {
		return value
	}
	policies := mapping()
	for _, name := range []string{"cache-call-unrestricted", "cache-operation", "cache-write-untrusted", "disallow-suppressions", "require-action-metadata", "require-commit-hash", "require-job-timeout", "missing-permissions"} {
		setField(policies, name, value)
	}
	return policies
}

// mergeMigration combines distinct fields but refuses to choose between two
// explicitly configured values. "default" levels introduced for knowledge
// options never override a user-specified diagnostic level.
func mergeMigration(current, legacy any, path, origin string) (any, error) {
	if !isMapping(current) && isMapping(legacy) && field(legacy, "level") != nil {
		wrapped := mapping()
		setField(wrapped, "level", current)
		current = wrapped
	}
	if strings.HasSuffix(path, ".level") {
		var a, b Level
		if err := decodeValue(current, &a); err != nil {
			return nil, err
		}
		if err := decodeValue(legacy, &b); err != nil {
			return nil, err
		}
		if a == b {
			return current, nil
		}
	}
	if isMapping(current) && isMapping(legacy) && !strings.HasPrefix(path, "repository.") {
		next := cloneMapping(current)
		for key, raw := range members(legacy) {
			value := raw
			if value == nil {
				value = explicitNull{}
			}
			if previous := field(current, key); previous != nil {
				if key == "level" && (text(value) == "default" || isNull(value)) {
					continue
				}
				merged, err := mergeMigration(previous, value, path+"."+key, origin)
				if err != nil {
					return nil, err
				}
				value = merged
			}
			setField(next, key, value)
		}
		return next, nil
	}
	var a, b any
	if err := decodeValue(current, &a); err != nil {
		return nil, err
	}
	if err := decodeValue(legacy, &b); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(a, b) {
		return nil, fmt.Errorf("cannot migrate %s: conflicts with %s; remove one setting or make their values agree", origin, path)
	}
	return current, nil
}

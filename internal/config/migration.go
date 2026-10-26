package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"go.yaml.in/yaml/v4"
)

var legacyKeys = []string{"policy", "self-hosted-runner", "config-variables", "config-secrets", "assume-default-permissions", "paths"}

// NormalizeForMerge expands and canonicalizes one document before inheritance
// merging. Plugin-rule binding is deferred until providers are installed.
// Callers must validate the resulting configuration before analysis.
func NormalizeForMerge(node *yaml.Node) (*yaml.Node, error) {
	expanded, err := configtree.Expand(node, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	canonical, _, err := migrateNode(normalizeToolSwitch(expanded))
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
func MigrateDocument(path string, content []byte) (Migration, error) {
	root, err := configtree.Decode(path, content)
	if err != nil {
		return Migration{}, err
	}
	root, err = configtree.Expand(root, map[*yaml.Node]bool{})
	if err != nil {
		return Migration{}, err
	}
	for _, warning := range configWarnings(root, nil) {
		if strings.Contains(warning.Message, `"self-hosted-runner.`) || strings.Contains(warning.Message, `"paths[`) {
			return Migration{}, fmt.Errorf("cannot migrate without dropping an unknown setting: %s", warning.Message)
		}
	}
	next, changed, err := migrateNode(root)
	if err != nil {
		return Migration{}, err
	}
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
	case ".json":
		var value map[string]any
		if err := next.Decode(&value); err != nil {
			return Migration{}, err
		}
		result.Content, err = json.MarshalIndent(value, "", "  ")
		if err != nil {
			return Migration{}, err
		}
	case ".toml":
		result.Content, err = migrateTOML(content, next)
		if err != nil {
			return Migration{}, err
		}
	default:
		var output bytes.Buffer
		encoder := yaml.NewEncoder(&output)
		encoder.SetIndent(2)
		if err := errors.Join(encoder.Encode(next), encoder.Close()); err != nil {
			return Migration{}, err
		}
		result.Content = output.Bytes()
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
func legacyExpressionControls(root *yaml.Node) bool {
	rules := field(field(root, "lint"), "rules")
	if field(field(rules, "correctness"), "expression") != nil {
		return true
	}
	if disabled := field(rules, "disable"); disabled != nil {
		for _, item := range disabled.Content {
			if item.Value == "expression" {
				return true
			}
		}
	}
	if ignores := field(root, "ignores"); ignores != nil {
		for _, item := range ignores.Content {
			if selected := field(item, "rule"); selected != nil && selected.Value == "expression" {
				return true
			}
		}
	}
	if overrides := field(root, "overrides"); overrides != nil {
		return slices.ContainsFunc(overrides.Content, legacyExpressionControls)
	}
	return false
}

func field(node *yaml.Node, key string) *yaml.Node {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return nil
}

func mapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func textNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func setField(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, textNode(key), value)
}

func cloneMapping(node *yaml.Node) *yaml.Node {
	if node == nil {
		return mapping()
	}
	next := *node
	next.Content = append([]*yaml.Node{}, node.Content...)
	return &next
}

func migrateNode(root *yaml.Node) (*yaml.Node, bool, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return root, false, nil
	}
	changed := false
	for _, key := range legacyKeys {
		changed = changed || field(root, key) != nil
	}
	if !changed {
		return root, false, nil
	}
	next := cloneMapping(root)
	lint := cloneMapping(field(root, "lint"))
	if lint.Kind != yaml.MappingNode {
		return nil, false, errors.New("cannot migrate legacy settings alongside non-mapping lint; remove the conflicting lint reset")
	}
	rules := cloneMapping(field(lint, "rules"))
	if rules.Kind != yaml.MappingNode {
		return nil, false, errors.New("cannot migrate legacy settings alongside non-mapping lint.rules")
	}
	install := func(name string, setting *yaml.Node, origin string) error {
		descriptor, _ := FindRule(name)
		group := cloneMapping(field(rules, descriptor.Group))
		if group.Kind != yaml.MappingNode {
			// Expand a valid group shorthand without dropping its baseline.
			var decoded Group
			if err := group.Decode(&decoded); err != nil {
				return err
			}
			group = mapping()
			if decoded.Level != "" {
				setField(group, "level", textNode(string(decoded.Level)))
			}
			if decoded.Preset != "" {
				setField(group, "preset", textNode(string(decoded.Preset)))
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
	option := func(name, key string, value *yaml.Node, origin string) error {
		setting, options := mapping(), mapping()
		setField(setting, "level", textNode("default"))
		setField(options, key, value)
		setField(setting, "options", options)
		return install(name, setting, origin)
	}
	for _, key := range legacyKeys {
		value := field(root, key)
		if value == nil {
			continue
		}
		var err error
		switch key {
		case "config-variables", "config-secrets":
			var names []string
			if err = value.Decode(&names); err == nil {
				err = option("expression-reference", strings.TrimPrefix(key, "config-"), value, key)
			}
		case "self-hosted-runner":
			var runner SelfHostedRunnerConfig
			if err = value.Decode(&runner); err == nil {
				labels := field(value, "labels")
				if value.ShortTag() == "!!null" {
					labels = value
				}
				if labels != nil {
					err = option("runner-label", "labels", labels, key)
				}
			}
		case "assume-default-permissions":
			var assumption DefaultPermissionsAssumption
			if err = value.Decode(&assumption); err == nil {
				err = option("workflow-call", "default-permissions", value, key)
			}
		case "paths":
			var paths map[string]PathConfig
			if err = value.Decode(&paths); err != nil {
				break
			}
			messages := mapping()
			if value.ShortTag() == "!!null" {
				messages = value
			} else {
				for i := 0; i < len(value.Content); i += 2 {
					pattern, config := value.Content[i], value.Content[i+1]
					ignored := field(config, "ignore")
					if ignored == nil {
						if config.Kind == yaml.MappingNode {
							// An empty mapping inherits the existing path entry;
							// replacing it with [] would erase its patterns.
							continue
						}
						ignored = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
					}
					messages.Content = append(messages.Content, pattern, ignored)
				}
			}
			if previous := field(lint, "message-ignores"); previous != nil {
				messages, err = mergeMigration(previous, messages, "lint.message-ignores", "paths")
			}
			setField(lint, "message-ignores", messages)
		case "policy":
			var policy Policy
			if err = value.Decode(&policy); err != nil {
				break
			}
			policies := value
			if value.ShortTag() == "!!null" {
				policies = mapping()
				for _, name := range []string{"cache-call-unrestricted", "cache-operation", "cache-write-untrusted", "disallow-suppressions", "require-commit-hash", "require-job-timeout", "require-permissions", "required-actions"} {
					setField(policies, name, value)
				}
			}
			for i := 0; i < len(policies.Content) && err == nil; i += 2 {
				name, selected := policies.Content[i].Value, policies.Content[i+1]
				setting := mapping()
				setting.Line, setting.Column = selected.Line, selected.Column
				level := "default"
				setField(setting, "options", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
				switch selected.ShortTag() {
				case "!!bool":
					level = "off"
					var enabled bool
					if err = selected.Decode(&enabled); err != nil {
						break
					}
					if enabled {
						level = "on"
					}
				case "!!map":
					level = "on"
					setField(setting, "options", selected)
				case "!!seq":
					level = "on"
					options := mapping()
					setField(options, "actions", selected)
					setField(setting, "options", options)
				}
				levelNode := textNode(level)
				levelNode.Line, levelNode.Column = selected.Line, selected.Column
				if selected.ShortTag() == "!!null" {
					levelNode = selected
					setField(setting, "options", selected)
				}
				setField(setting, "level", levelNode)
				err = install(name, setting, "policy."+name)
			}
		}
		if err != nil {
			return nil, false, err
		}
		for i := 0; i < len(next.Content); i += 2 {
			if next.Content[i].Value == key {
				next.Content = append(next.Content[:i], next.Content[i+2:]...)
				break
			}
		}
	}
	if len(rules.Content) != 0 {
		setField(lint, "rules", rules)
	}
	setField(next, "lint", lint)
	return next, true, nil
}

// mergeMigration combines distinct fields but refuses to choose between two
// explicitly configured values. "default" levels introduced for knowledge
// options never override a user-specified diagnostic level.
func mergeMigration(current, legacy *yaml.Node, path, origin string) (*yaml.Node, error) {
	if current.Kind == yaml.ScalarNode && legacy.Kind == yaml.MappingNode && field(legacy, "level") != nil {
		wrapped := mapping()
		setField(wrapped, "level", current)
		current = wrapped
	}
	if strings.HasSuffix(path, ".level") {
		var a, b Level
		if err := current.Decode(&a); err != nil {
			return nil, err
		}
		if err := legacy.Decode(&b); err != nil {
			return nil, err
		}
		if a == b {
			return current, nil
		}
	}
	if current.Kind == yaml.MappingNode && legacy.Kind == yaml.MappingNode {
		next := cloneMapping(current)
		for i := 0; i < len(legacy.Content); i += 2 {
			key, value := legacy.Content[i].Value, legacy.Content[i+1]
			if previous := field(current, key); previous != nil {
				if key == "level" && (value.Value == "default" || value.ShortTag() == "!!null") {
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
	if err := current.Decode(&a); err != nil {
		return nil, err
	}
	if err := legacy.Decode(&b); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(a, b) {
		return nil, fmt.Errorf("cannot migrate %s: conflicts with %s; remove one setting or make their values agree", origin, path)
	}
	return current, nil
}

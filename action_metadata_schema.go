package actionlint

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

type actionSchemaKind uint8

const (
	actionSchemaString actionSchemaKind = iota
	actionSchemaBoolean
	actionSchemaMapping
	actionSchemaSequence
	actionSchemaUnion
)

// These definitions retain the runner's structural constraints independently of
// the metadata fields needed for checking workflow callers. Required execution
// fields and runtime-specific diagnostics remain in RuleAction.
type actionSchemaDefinition struct {
	kind       actionSchemaKind
	properties map[string]string
	required   []string
	loose      string
	item       string
	variants   []string
	nonEmpty   bool
}

func actionSchemaNode(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil && n.Alias.Kind != yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// actionSchemaScalar mirrors TemplateReader.Validate: null, booleans and
// numbers are converted to strings when a string definition is expected.
func actionSchemaScalar(n *yaml.Node) *string {
	v := actionSchemaLiteralScalar(n)
	if v != nil {
		if literal := literalExpressionValue(*v); literal != nil {
			return literal
		}
	}
	return v
}

func actionSchemaLiteralScalar(n *yaml.Node) *string {
	n = actionSchemaNode(n)
	if n == nil || n.Kind != yaml.ScalarNode {
		return nil
	}
	v := n.Value
	if n.Tag == "!!null" {
		v = ""
	}
	return &v
}

func (rule *RuleAction) checkActionMetadataSchema(meta *ActionMetadata) {
	if meta.schemaRoot == nil {
		return
	}
	// Inspect physical YAML nodes once. Alias targets already occur in this tree;
	// following aliases here would recurse through self-referential anchors.
	var checkTags func(*yaml.Node)
	checkTags = func(n *yaml.Node) {
		if message := rawYAMLTagError(n); message != "" {
			rule.metadataErrorfAt(meta, n.Line, n.Column, "%s in action metadata", message)
		}
		for _, child := range n.Content {
			checkTags(child)
		}
	}
	checkTags(meta.schemaRoot)
	rule.checkActionSchemaValue(meta, meta.schemaRoot, "action-root", "", 0, false)
}

func (rule *RuleAction) actionSchemaError(meta *ActionMetadata, n *yaml.Node, path, message string) {
	rule.metadataErrorfAt(meta, n.Line, n.Column, "%s at %q in action metadata", message, path)
}

func (rule *RuleAction) checkActionSchemaValue(meta *ActionMetadata, node *yaml.Node, definition, path string, depth int, evaluated bool) {
	n := actionSchemaNode(node)
	if n == nil || definition == "any" {
		return
	}
	// Match the runner's TemplateMemory maximum depth; insertion mappings may
	// otherwise revisit the same YAML anchor indefinitely.
	if depth > 100 {
		rule.actionSchemaError(meta, n, path, "action metadata exceeds maximum nesting depth of 100")
		return
	}
	d, ok := actionMetadataSchema[definition]
	if !ok && definition != "string" {
		return
	}
	if d.kind == actionSchemaUnion {
		variant := ""
		switch definition {
		case "runs":
			switch strings.ToLower(meta.Runs.Using) {
			case "docker":
				variant = "container-runs"
			case "composite":
				variant = "composite-runs"
			default:
				variant = "node-runs"
				if meta.Runs.Plugin != "" && meta.Runs.Using == "" {
					variant = "plugin-runs"
				}
			}
		case "composite-step":
			variant = "uses-step"
			for i := 0; i+1 < len(n.Content); i += 2 {
				key := n.Content[i].Value
				if !evaluated {
					key = actionMetadataKey(n.Content[i])
				}
				if strings.EqualFold(key, "run") {
					variant = "run-step"
				}
			}
		}
		if slices.Contains(d.variants, variant) {
			rule.checkActionSchemaValue(meta, n, variant, path, depth, evaluated)
		}
		return
	}

	// Input/composite checks provide specific hints; other fields use the shared availability table.
	if value := yamlExprString(n); !evaluated && value != nil && strings.Contains(value.Value, "${{") {
		if _, allowed := actionMetadataAvailability[path]; allowed {
			if !strings.HasPrefix(path, "runs.steps.*.") && path != "inputs.*.default" {
				for _, violation := range actionExpressionViolations(value.Value, false, path) {
					message := violation.message
					if violation.context != "" {
						message = fmt.Sprintf("context %q is not available", violation.context)
					}
					rule.actionSchemaError(meta, n, path, message)
				}
			}
			if definition == "step-if" {
				return // Conditions use expression truthiness, including object and array values.
			}
			if literal, known := workflowExpressionLiteral(&String{Value: value.Value}); known {
				var result yaml.Node
				if err := result.Encode(literal); err != nil {
					rule.actionSchemaError(meta, n, path, fmt.Sprintf("could not validate expression value: %v", err))
					return
				}
				actionSchemaSetPosition(&result, n.Line, n.Column)
				n, evaluated = &result, true
			} else if d.kind == actionSchemaString || parseAssignedExpression(value.Value) != nil {
				return // The runner validates the evaluated value's type at runtime.
			}
		} else if !actionLiteralExpression(value.Value) {
			// A literal string expression is an escape, accepted even when expressions
			// are forbidden. It becomes a literal token before schema validation.
			rule.actionSchemaError(meta, n, path, "expressions are not allowed")
		}
	}

	// Evaluated strings and keys are data, including any expression delimiters.
	scalar := actionSchemaScalar
	if evaluated {
		scalar = actionSchemaLiteralScalar
	}
	switch d.kind {
	case actionSchemaString:
		v := scalar(n)
		if v == nil {
			if evaluated || path != "runs.steps.*.run" && path != "runs.steps.*.shell" && path != "runs.steps.*.uses" {
				rule.actionSchemaError(meta, n, path, "expected a scalar string")
			}
		} else if d.nonEmpty && *v == "" && path != "runs.steps.*.uses" {
			rule.actionSchemaError(meta, n, path, "expected a non-empty string")
		}
	case actionSchemaBoolean:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
			rule.actionSchemaError(meta, n, path, "expected a boolean or expression")
		}
	case actionSchemaSequence:
		if n.Kind != yaml.SequenceNode {
			// Missing/null composite steps already has a precise runtime diagnostic.
			if path != "runs.steps" {
				rule.actionSchemaError(meta, n, path, "expected a sequence")
			}
			return
		}
		for _, value := range n.Content {
			rule.checkActionSchemaValue(meta, value, d.item, path+".*", depth+1, evaluated)
		}
	case actionSchemaMapping:
		if n.Kind != yaml.MappingNode {
			if path != "runs.steps.*" && (path != "runs" || n.Kind != yaml.ScalarNode || n.Tag != "!!null") {
				rule.actionSchemaError(meta, n, path, "expected a mapping")
			}
			return
		}
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if expr := yamlExprString(key); !evaluated && expr != nil && strings.Contains(expr.Value, "${{") && !actionLiteralExpression(expr.Value) {
				if _, allowed := actionMetadataAvailability[path]; !allowed {
					rule.actionSchemaError(meta, key, path, "expressions are not allowed in keys")
				} else if strings.TrimSpace(expr.Value) == "${{ insert }}" {
					// The template reader treats insert as a mapping directive,
					// whose value is another mapping or a mapping expression.
					rule.checkActionSchemaValue(meta, value, definition, path, depth+1, evaluated)
					continue
				} else {
					for _, violation := range actionExpressionViolations(expr.Value, false, path) {
						message := violation.message
						if violation.context != "" {
							message = fmt.Sprintf("context %q is not available in keys", violation.context)
						}
						rule.actionSchemaError(meta, key, path, message)
					}
				}
			}
			name := scalar(key)
			if name == nil || *name == "" {
				rule.actionSchemaError(meta, key, path, "expected a non-empty scalar key")
				continue
			}
			lower := strings.ToLower(*name)
			if seen[lower] {
				rule.actionSchemaError(meta, key, path, fmt.Sprintf("duplicate key %q", *name))
			}
			seen[lower] = true
			child, known := d.properties[lower]
			childPath := strings.TrimPrefix(path+"."+lower, ".")
			if !known {
				child, childPath = d.loose, path+".*"
				if child == "" {
					// Existing step diagnostics also explain the allowed keys. Runs
					// fields with values are already checked against the runtime.
					if path != "runs.steps.*" && (path != "runs" || meta.Runs.Using == "" || !actionRunsPropertyChecked(meta, lower)) {
						rule.actionSchemaError(meta, key, path, fmt.Sprintf("unexpected key %q", *name))
					}
					continue
				}
			}
			rule.checkActionSchemaValue(meta, value, child, childPath, depth+1, evaluated)
		}
		for _, key := range d.required {
			if !seen[key] && (path != "runs.steps.*" || !slices.Contains([]string{"run", "shell", "uses"}, key)) {
				rule.actionSchemaError(meta, n, path, fmt.Sprintf("missing required key %q", key))
			}
		}
	}
}

func actionSchemaSetPosition(n *yaml.Node, line, column int) {
	n.Line, n.Column = line, column
	for _, child := range n.Content {
		actionSchemaSetPosition(child, line, column)
	}
}

func actionRunsPropertyChecked(meta *ActionMetadata, key string) bool {
	r := &meta.Runs
	switch key {
	case "main":
		return r.Main != ""
	case "pre":
		return r.Pre != ""
	case "post":
		return r.Post != ""
	case "pre-if":
		return r.PreIf != ""
	case "post-if":
		return r.PostIf != ""
	case "steps":
		return len(r.Steps) > 0
	case "image":
		return r.Image != ""
	case "entrypoint":
		return r.Entrypoint != ""
	case "pre-entrypoint":
		return r.PreEntrypoint != ""
	case "post-entrypoint":
		return r.PostEntrypoint != ""
	case "args":
		return r.Args != nil
	case "env":
		return r.Env != nil
	default:
		return false
	}
}

func actionLiteralExpression(s string) bool {
	return literalExpressionValue(s) != nil
}

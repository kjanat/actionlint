package actionmeta

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/calls"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type actionSchemaKind uint8

const (
	actionSchemaString actionSchemaKind = iota
	actionSchemaBoolean
	actionSchemaMapping
	actionSchemaSequence
	actionSchemaUnion
)

type actionSchemaDefinition struct {
	kind       actionSchemaKind
	properties map[string]string
	required   []string
	loose      string
	item       string
	variants   []string
	nonEmpty   bool
}

// Node resolves one YAML alias level without following recursive anchors.
func Node(n *codec.Node) *codec.Node {
	for n != nil && n.Kind == codec.AliasNode && n.Alias != nil && n.Alias.Kind != codec.AliasNode {
		n = n.Alias
	}
	return n
}

// Scalar mirrors TemplateReader.Validate: null, booleans and
// numbers are converted to strings when a string definition is expected.
// Scalar returns the runner-compatible scalar value, including literal expressions.
func Scalar(n *codec.Node) *string {
	v := actionSchemaLiteralScalar(n)
	if v != nil {
		if literal := syntax.LiteralExpressionValue(*v); literal != nil {
			return literal
		}
	}
	return v
}

func actionSchemaLiteralScalar(n *codec.Node) *string {
	n = Node(n)
	if n == nil || n.Kind != codec.ScalarNode {
		return nil
	}
	v := n.Value
	if n.Tag == "!!null" {
		v = ""
	}
	return &v
}

func (rule *schemaChecker) checkActionMetadataSchema(meta *Metadata) {
	if meta.Root == nil {
		return
	}
	// Inspect physical YAML nodes once. Alias targets already occur in this tree;
	// following aliases here would recurse through self-referential anchors.
	var checkSyntax func(*codec.Node)
	checkSyntax = func(n *codec.Node) {
		if n.Anchor != "" {
			// Property diagnostics use the node start, including any preceding tag.
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, CodeYAMLAnchor, "YAML anchors are not supported in action metadata; remove anchor %q", n.Anchor)
		}
		if n.Kind == codec.AliasNode {
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, CodeYAMLAlias, "YAML aliases are not supported in action metadata; replace alias %q", n.Value)
		}
		if message := parser.RawYAMLTagError(n); message != "" {
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, CodeYAMLTag, "%s in action metadata", message)
		}
		for _, child := range n.Content {
			checkSyntax(child)
		}
	}
	checkSyntax(meta.Root)
	if err := parser.AdmitYAMLAliases(meta.Root); err != nil {
		return
	}
	rule.checkActionSchemaValue(meta, meta.Root, "action-root", schemaPath{}, 0, false)
}

func (rule *schemaChecker) actionSchemaError(meta *Metadata, n *codec.Node, path schemaPath, code ProblemCode, message string) {
	rule.metadataErrorfAt(meta, n.Line, n.Column, nil, code, "%s at \"%s\" in action metadata", message, path.Display())
}

func (rule *schemaChecker) checkActionSchemaValue(meta *Metadata, node *codec.Node, definitionName string, location schemaPath, depth int, evaluated bool) {
	rule.checkActionSchemaMappingValue(meta, node, definitionName, location, depth, evaluated, nil)
}

// Insertions share their enclosing mapping namespace; child values start a new one.
func (rule *schemaChecker) checkActionSchemaMappingValue(meta *Metadata, node *codec.Node, definitionName string, location schemaPath, depth int, evaluated bool, seen map[string]bool) {
	path := location.Schema()
	n := Node(node)
	if n == nil || definitionName == "any" {
		return
	}
	visit := actionSchemaVisit{node: n, definition: definitionName}
	if rule.active[visit] {
		return
	}
	if rule.active == nil {
		rule.active = make(map[actionSchemaVisit]bool)
	}
	rule.active[visit] = true
	defer delete(rule.active, visit)
	// Match the runner's TemplateMemory maximum depth; insertion mappings may
	// otherwise revisit the same YAML anchor indefinitely.
	if depth > 100 {
		rule.actionSchemaError(meta, n, location, CodeNestingDepth, "action metadata exceeds maximum nesting depth of 100")
		return
	}
	d, ok := actionMetadataSchema[definitionName]
	if !ok && definitionName != "string" {
		return
	}
	if d.kind == actionSchemaUnion {
		variant := ""
		switch definitionName {
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
					key = Key(n.Content[i])
				}
				if strings.EqualFold(key, "run") {
					variant = "run-step"
				}
			}
		}
		if slices.Contains(d.variants, variant) {
			rule.checkActionSchemaMappingValue(meta, n, variant, location, depth, evaluated, seen)
		}
		return
	}

	// Input/composite checks provide specific hints; other fields use the shared availability table.
	if value := yamlExprString(n); !evaluated && value != nil && strings.Contains(value.Value, "${{") {
		if _, allowed := actionMetadataAvailability[path]; allowed {
			if !strings.HasPrefix(path, "runs.steps.*.") && path != "inputs.*.default" {
				for _, violation := range rule.expressionFindings(value, path, location.Display(), false, meta.Inputs) {
					message := violation.Message
					if violation.Context != "" {
						message = fmt.Sprintf("context %q is not available", violation.Context)
					}
					pos := ExpressionPosition(meta, value, violation.Offset)
					rule.metadataErrorfAt(meta, pos.Line, pos.Column, ExpressionEndPosition(meta, value, violation), violation.Code, "%s at \"%s\" in action metadata", message, location.Display())
				}
			}
			if definitionName == "step-if" {
				return // Conditions use expression truthiness, including object and array values.
			}
			if literal, known := definition.WorkflowExpressionLiteral(&definition.String{Value: value.Value}); known {
				result, err := codec.Encode(literal)
				if err != nil {
					rule.actionSchemaError(meta, n, location, CodeExpressionType, fmt.Sprintf("could not validate expression value: %v", err))
					return
				}
				actionSchemaSetPosition(result, n.Line, n.Column)
				n, evaluated = result, true
			} else if d.kind == actionSchemaString || syntax.ParseAssignedExpression(value.Value) != nil {
				return // The runner validates the evaluated value's type at runtime.
			}
		} else if !actionLiteralExpression(value.Value) {
			// A literal string expression is an escape, accepted even when expressions
			// are forbidden. It becomes a literal token before schema validation.
			rule.actionSchemaError(meta, n, location, CodeExpressionForbidden, "expressions are not allowed")
		}
	}

	// Evaluated strings and keys are data, including any expression delimiters.
	scalar := Scalar
	if evaluated {
		scalar = actionSchemaLiteralScalar
	}
	switch d.kind {
	case actionSchemaString:
		v := scalar(n)
		if v == nil {
			if evaluated || path != "runs.steps.*.run" && path != "runs.steps.*.shell" && path != "runs.steps.*.uses" {
				rule.actionSchemaError(meta, n, location, CodeInvalidFieldType, "expected a scalar string")
			}
		} else if d.nonEmpty && *v == "" && path != "runs.steps.*.uses" {
			rule.actionSchemaError(meta, n, location, CodeInvalidFieldType, "expected a non-empty string")
		}
	case actionSchemaBoolean:
		if n.Kind != codec.ScalarNode || n.Tag != "!!bool" {
			rule.actionSchemaError(meta, n, location, CodeInvalidFieldType, "expected a boolean or expression")
		}
	case actionSchemaSequence:
		if n.Kind != codec.SequenceNode {
			// Missing or null composite steps already have a specific runtime finding.
			if path != "runs.steps" {
				rule.actionSchemaError(meta, n, location, CodeInvalidFieldType, "expected a sequence")
			}
			return
		}
		for index, value := range n.Content {
			rule.checkActionSchemaValue(meta, value, d.item, location.Index(index), depth+1, evaluated)
		}
	case actionSchemaMapping:
		if n.Kind != codec.MappingNode {
			if path != "runs.steps.*" && (path != "runs" || n.Kind != codec.ScalarNode || n.Tag != "!!null") {
				rule.actionSchemaError(meta, n, location, CodeInvalidFieldType, "expected a mapping")
			}
			return
		}
		ownMapping := seen == nil
		if ownMapping {
			seen = map[string]bool{}
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if expr := yamlExprString(key); !evaluated && expr != nil && strings.Contains(expr.Value, "${{") && !actionLiteralExpression(expr.Value) {
				if _, allowed := actionMetadataAvailability[path]; !allowed {
					rule.actionSchemaError(meta, key, location, CodeExpressionForbidden, "expressions are not allowed in keys")
				} else if syntax.IsMappingInsertion(expr.Value) {
					// The template reader treats insert as a mapping directive,
					// whose value is another mapping or a mapping expression.
					rule.checkActionSchemaMappingValue(meta, value, definitionName, location, depth+1, evaluated, seen)
					continue
				} else {
					for _, violation := range rule.expressionFindings(expr, path, location.Display(), true, meta.Inputs) {
						message := violation.Message
						if violation.Context != "" {
							message = fmt.Sprintf("context %q is not available in keys", violation.Context)
						}
						pos := ExpressionPosition(meta, expr, violation.Offset)
						rule.metadataErrorfAt(meta, pos.Line, pos.Column, ExpressionEndPosition(meta, expr, violation), violation.Code, "%s at \"%s\" in action metadata", message, location.Display())
					}
				}
			}
			name := scalar(key)
			if name == nil || *name == "" {
				rule.actionSchemaError(meta, key, location, CodeInvalidFieldType, "expected a non-empty scalar key")
				continue
			}
			lower := strings.ToLower(*name)
			if seen[lower] {
				rule.actionSchemaError(meta, key, location, CodeDuplicateField, fmt.Sprintf("duplicate key %q", *name))
			}
			seen[lower] = true
			child, known := d.properties[lower]
			childPath := location.Key(*name)
			if !known {
				child, childPath = d.loose, location.LooseKey(*name)
				if child == "" {
					// Existing step diagnostics also explain the allowed keys. Runs
					// fields with values are already checked against the runtime.
					if path != "runs.steps.*" && (path != "runs" || meta.Runs.Using == "" || !actionRunsPropertyChecked(meta, lower)) {
						rule.actionSchemaError(meta, key, location, CodeUnexpectedField, fmt.Sprintf("unexpected key %q", *name))
					}
					continue
				}
			}
			rule.checkActionSchemaValue(meta, value, child, childPath, depth+1, evaluated)
		}
		for _, key := range d.required {
			if ownMapping && !seen[key] && (path != "runs.steps.*" || !slices.Contains([]string{"run", "shell", "uses"}, key)) {
				rule.actionSchemaError(meta, n, location, CodeMissingField, fmt.Sprintf("missing required key %q", key))
			}
		}
	case actionSchemaUnion:
		// Unions dispatch to a concrete variant before value checks run.
	}
}

func actionSchemaSetPosition(n *codec.Node, line, column int) {
	n.Line, n.Column = line, column
	for _, child := range n.Content {
		actionSchemaSetPosition(child, line, column)
	}
}

func actionRunsPropertyChecked(meta *Metadata, key string) bool {
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
	return syntax.LiteralExpressionValue(s) != nil
}

// ExpressionField records a scalar expression found during manifest validation.
// Value contains the decoded text and its original YAML position.
type ExpressionField struct {
	Value ExprString
	// SchemaPath selects availability rules, for example outputs.*.value.
	SchemaPath string
	// FieldPath appears in diagnostics, for example outputs.result.value.
	FieldPath string
	// IsKey distinguishes mapping-key expressions from value expressions.
	IsKey       bool
	Expressions []ParsedExpression
}

// MetadataValidation separates structural problems from expression fields for later checks.
type MetadataValidation struct {
	Problems         []MetadataProblem
	ExpressionFields []ExpressionField
}

// Validate checks manifest structure and records expression fields without applying lint policy.
func Validate(meta *Metadata) MetadataValidation {
	checker := &schemaChecker{}
	checker.checkActionMetadataSchema(meta)
	return MetadataValidation{Problems: checker.problems, ExpressionFields: checker.expressions}
}

type actionSchemaVisit struct {
	node       *codec.Node
	definition string
}

type schemaChecker struct {
	problems    []MetadataProblem
	active      map[actionSchemaVisit]bool
	expressions []ExpressionField
}

func (rule *schemaChecker) expressionFindings(value *ExprString, field, location string, key bool, inputs calls.Inputs) []ExpressionProblem {
	read := ReadExpressions(value.Value, false, field, inputs)
	rule.expressions = append(rule.expressions, ExpressionField{Value: *value, SchemaPath: field, FieldPath: location, IsKey: key, Expressions: read.Expressions})
	return read.Problems
}

func (rule *schemaChecker) metadataErrorfAt(meta *Metadata, line, column int, end *sourcetext.Position, code ProblemCode, format string, args ...any) {
	d := MetadataProblem{Path: meta.Path(), Start: sourcetext.Position{Line: line, Column: column}, Code: code, Message: fmt.Sprintf(format, args...)}
	if end != nil {
		d.End = *end
	}
	rule.problems = append(rule.problems, d)
}

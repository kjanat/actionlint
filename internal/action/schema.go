package action

import (
	"fmt"
	"slices"
	"strings"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/schemapath"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
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
func Node(n *syntax.Node) *syntax.Node {
	for n != nil && n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}
	return n
}

// Scalar mirrors TemplateReader.Validate: null, booleans and
// numbers are converted to strings when a string definition is expected.
// Scalar returns the runner-compatible scalar value, including literal expressions.
func Scalar(n *syntax.Node) *string {
	v := actionSchemaLiteralScalar(n)
	if v != nil {
		if literal := exprlang.LiteralExpressionValue(*v); literal != nil {
			return literal
		}
	}
	return v
}

func actionSchemaLiteralScalar(n *syntax.Node) *string {
	n = Node(n)
	if n == nil || n.Kind != syntax.ScalarNode {
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
	var checkSyntax func(*syntax.Node)
	checkSyntax = func(n *syntax.Node) {
		if n.Anchor != "" {
			// Property diagnostics use the node start, including any preceding tag.
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, "YAML anchors are not supported in action metadata; remove anchor %q", n.Anchor)
		}
		if n.Kind == syntax.AliasNode {
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, "YAML aliases are not supported in action metadata; replace alias %q", n.Value)
		}
		if message := workflowparser.RawYAMLTagError(n); message != "" {
			rule.metadataErrorfAt(meta, n.Line, n.Column, nil, "%s in action metadata", message)
		}
		for _, child := range n.Content {
			checkSyntax(child)
		}
	}
	checkSyntax(meta.Root)
	if err := workflowparser.AdmitYAMLAliases(meta.Root); err != nil {
		return
	}
	rule.checkActionSchemaValue(meta, meta.Root, "action-root", schemapath.Path{}, 0, false)
}

func (rule *schemaChecker) actionSchemaError(meta *Metadata, n *syntax.Node, path schemapath.Path, message string) {
	rule.metadataErrorfAt(meta, n.Line, n.Column, nil, "%s at \"%s\" in action metadata", message, path.Display())
}

func (rule *schemaChecker) checkActionSchemaValue(meta *Metadata, node *syntax.Node, definition string, location schemapath.Path, depth int, evaluated bool) {
	rule.checkActionSchemaMappingValue(meta, node, definition, location, depth, evaluated, nil)
}

// Insertions share their enclosing mapping namespace; child values start a new one.
func (rule *schemaChecker) checkActionSchemaMappingValue(meta *Metadata, node *syntax.Node, definition string, location schemapath.Path, depth int, evaluated bool, seen map[string]bool) {
	path := location.Schema()
	n := Node(node)
	if n == nil || definition == "any" {
		return
	}
	visit := actionSchemaVisit{node: n, definition: definition}
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
		rule.actionSchemaError(meta, n, location, "action metadata exceeds maximum nesting depth of 100")
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
				for _, violation := range ExpressionViolations(value.Value, false, path, meta.Inputs) {
					message := violation.Message
					if violation.Context != "" {
						message = fmt.Sprintf("context %q is not available", violation.Context)
					}
					pos := ExpressionPosition(meta, value, violation.Offset)
					rule.metadataErrorfAt(meta, pos.Line, pos.Col, ExpressionEndPosition(meta, value, violation), "%s at \"%s\" in action metadata", message, location.Display())
				}
			}
			if definition == "step-if" {
				return // Conditions use expression truthiness, including object and array values.
			}
			if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: value.Value}); known {
				result, err := codec.Encode(literal)
				if err != nil {
					rule.actionSchemaError(meta, n, location, fmt.Sprintf("could not validate expression value: %v", err))
					return
				}
				actionSchemaSetPosition(result, n.Line, n.Column)
				n, evaluated = result, true
			} else if d.kind == actionSchemaString || exprlang.ParseAssignedExpression(value.Value) != nil {
				return // The runner validates the evaluated value's type at runtime.
			}
		} else if !actionLiteralExpression(value.Value) {
			// A literal string expression is an escape, accepted even when expressions
			// are forbidden. It becomes a literal token before schema validation.
			rule.actionSchemaError(meta, n, location, "expressions are not allowed")
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
				rule.actionSchemaError(meta, n, location, "expected a scalar string")
			}
		} else if d.nonEmpty && *v == "" && path != "runs.steps.*.uses" {
			rule.actionSchemaError(meta, n, location, "expected a non-empty string")
		}
	case actionSchemaBoolean:
		if n.Kind != syntax.ScalarNode || n.Tag != "!!bool" {
			rule.actionSchemaError(meta, n, location, "expected a boolean or expression")
		}
	case actionSchemaSequence:
		if n.Kind != syntax.SequenceNode {
			// Missing/null composite steps already has a precise runtime diagnostic.
			if path != "runs.steps" {
				rule.actionSchemaError(meta, n, location, "expected a sequence")
			}
			return
		}
		for index, value := range n.Content {
			rule.checkActionSchemaValue(meta, value, d.item, location.Index(index), depth+1, evaluated)
		}
	case actionSchemaMapping:
		if n.Kind != syntax.MappingNode {
			if path != "runs.steps.*" && (path != "runs" || n.Kind != syntax.ScalarNode || n.Tag != "!!null") {
				rule.actionSchemaError(meta, n, location, "expected a mapping")
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
					rule.actionSchemaError(meta, key, location, "expressions are not allowed in keys")
				} else if exprlang.IsMappingInsertion(expr.Value) {
					// The template reader treats insert as a mapping directive,
					// whose value is another mapping or a mapping expression.
					rule.checkActionSchemaMappingValue(meta, value, definition, location, depth+1, evaluated, seen)
					continue
				} else {
					for _, violation := range ExpressionViolations(expr.Value, false, path, meta.Inputs) {
						message := violation.Message
						if violation.Context != "" {
							message = fmt.Sprintf("context %q is not available in keys", violation.Context)
						}
						pos := ExpressionPosition(meta, expr, violation.Offset)
						rule.metadataErrorfAt(meta, pos.Line, pos.Col, ExpressionEndPosition(meta, expr, violation), "%s at \"%s\" in action metadata", message, location.Display())
					}
				}
			}
			name := scalar(key)
			if name == nil || *name == "" {
				rule.actionSchemaError(meta, key, location, "expected a non-empty scalar key")
				continue
			}
			lower := strings.ToLower(*name)
			if seen[lower] {
				rule.actionSchemaError(meta, key, location, fmt.Sprintf("duplicate key %q", *name))
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
						rule.actionSchemaError(meta, key, location, fmt.Sprintf("unexpected key %q", *name))
					}
					continue
				}
			}
			rule.checkActionSchemaValue(meta, value, child, childPath, depth+1, evaluated)
		}
		for _, key := range d.required {
			if ownMapping && !seen[key] && (path != "runs.steps.*" || !slices.Contains([]string{"run", "shell", "uses"}, key)) {
				rule.actionSchemaError(meta, n, location, fmt.Sprintf("missing required key %q", key))
			}
		}
	}
}

func actionSchemaSetPosition(n *syntax.Node, line, column int) {
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
	return exprlang.LiteralExpressionValue(s) != nil
}

// Validate checks manifest structure independently of repository and caller state.
func Validate(meta *Metadata) []diagnostic.Diagnostic {
	checker := &schemaChecker{}
	checker.checkActionMetadataSchema(meta)
	return checker.findings
}

type actionSchemaVisit struct {
	node       *syntax.Node
	definition string
}

type schemaChecker struct {
	findings []diagnostic.Diagnostic
	active   map[actionSchemaVisit]bool
}

func (rule *schemaChecker) metadataErrorfAt(meta *Metadata, line, column int, end *workflowlang.Pos, format string, args ...any) {
	d := diagnostic.Diagnostic{Path: meta.Path(), Start: diagnostic.Position{Line: line, Column: column}, Rule: "action", Message: fmt.Sprintf(format, args...)}
	if end != nil {
		d.End = diagnostic.Position{Line: end.Line, Column: end.Col}
	}
	rule.findings = append(rule.findings, d)
}

package expressions

import (
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/internal/workflow/matrix"
)

type scriptValueProof uint8

const (
	scriptUnsafe scriptValueProof = iota
	scriptArgumentOnly
	scriptLiteralWord
)

func (proof scriptValueProof) safe(argument bool) bool {
	return proof == scriptLiteralWord || argument && proof == scriptArgumentOnly
}

func scriptLiteralProof(value string) scriptValueProof {
	if exprlang.SafeScriptWord(value) {
		return scriptLiteralWord
	}
	if exprlang.SafeScriptLiteral(value) {
		return scriptArgumentOnly
	}
	return scriptUnsafe
}

func (rule *Checker) safeScriptEnv(name string, argument bool) bool {
	var workflowEnv *workflowlang.Env
	if rule.workflow != nil {
		workflowEnv = rule.workflow.Env
	}
	for _, env := range []*workflowlang.Env{rule.scriptEnv, rule.jobEnv, workflowEnv} {
		if env == nil {
			continue
		}
		mapping := workflowlang.EnvMappingView(env)
		names, complete := workflowlang.InputResolver.Names(mapping)
		if !complete {
			return false
		}
		if _, present := names[strings.ToLower(name)]; present {
			value, known := workflowlang.InputResolver.Value(mapping, name)
			return known && scriptLiteralProof(value).safe(argument)
		}
	}
	return false
}

// safeMatrixScriptReferences proves individual paths across all declared axis
// values and include assignments, without expanding the Cartesian product.
// Exclusions can only remove values, so ignoring them keeps the proof conservative.
func safeMatrixScriptReferences(matrix *workflowlang.Matrix) map[string]scriptValueProof {
	if matrix == nil {
		return nil
	}
	resolved, complete := matrixcheck.Expand(matrix)
	if !complete {
		return nil
	}
	includes, complete := matrixcheck.ExpandCombinations(resolved.Include)
	if !complete {
		return nil
	}
	axes := map[string]map[string]scriptValueProof{}
	add := func(name string, value workflowlang.RawYAMLValue, expressions bool) bool {
		if !scriptPathName(name) {
			return true
		}
		paths := map[string]scriptValueProof{}
		if !matrixScriptValuePaths("matrix."+name, value, expressions, paths) {
			return false
		}
		if previous, exists := axes[name]; exists {
			// Missing members evaluate to an empty string. Only a present unsafe
			// value invalidates a proof from another alternative.
			for path, proof := range paths {
				before, present := previous[path]
				if present {
					proof = min(proof, before)
				}
				previous[path] = proof
			}
		} else {
			axes[name] = paths
		}
		return true
	}
	for name, row := range resolved.Rows {
		elements, complete := matrixcheck.RowElements(row, true)
		if !complete {
			return nil
		}
		for _, element := range elements {
			if !add(name, element.Value, element.Expressions) {
				return nil
			}
		}
	}
	if includes != nil {
		for _, combination := range includes.Combinations {
			for key, assignment := range combination.Assigns {
				name, known := matrixcheck.RowName(key, &workflowlang.MatrixRow{Name: assignment.Key, Evaluated: combination.Evaluated})
				if !known || !add(name, assignment.Value, !combination.Evaluated) {
					return nil
				}
			}
		}
	}
	result := map[string]scriptValueProof{}
	for _, paths := range axes {
		for path, proof := range paths {
			if proof != scriptUnsafe {
				result[path] = proof
			}
		}
	}
	return result
}

func matrixScriptValuePaths(path string, value workflowlang.RawYAMLValue, expressions bool, safe map[string]scriptValueProof) bool {
	switch value := value.(type) {
	case *workflowlang.RawYAMLString:
		if expressions && exprlang.ContainsExpression(value.Value) {
			literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: value.Value, Pos: value.Pos()})
			if !known {
				return false
			}
			return matrixScriptLiteralPaths(path, literal, safe)
		} else {
			return matrixScriptLiteralPaths(path, value.ScalarValue(), safe)
		}
	case *workflowlang.RawYAMLObject:
		safe[path] = scriptUnsafe
		allSafe := scriptLiteralWord
		for name, property := range value.Props {
			if expressions && exprlang.ContainsExpression(name) {
				return false
			}
			paths := map[string]scriptValueProof{}
			if !matrixScriptValuePaths("", property, expressions, paths) {
				return false
			}
			allSafe = min(allSafe, paths[""])
			if scriptPathName(name) {
				copyScriptPaths(path+"."+strings.ToLower(name), paths, safe)
			}
		}
		safe[path+"[*]"] = allSafe
	case *workflowlang.RawYAMLArray:
		safe[path] = scriptUnsafe
		allSafe := scriptLiteralWord
		for index, element := range value.Elems {
			child := path + "[" + strconv.Itoa(index) + "]"
			if !matrixScriptValuePaths(child, element, expressions, safe) {
				return false
			}
			allSafe = min(allSafe, safe[child])
		}
		safe[path+"[*]"] = allSafe
	}
	return true
}

func matrixScriptLiteralPaths(path string, value any, safe map[string]scriptValueProof) bool {
	switch value := value.(type) {
	case string:
		safe[path] = scriptLiteralProof(value)
	case nil, bool, float64:
		safe[path] = scriptArgumentOnly
	case map[string]any:
		safe[path] = scriptUnsafe
		names := map[string]bool{}
		for name := range value {
			key := strings.ToLower(name)
			if names[key] {
				// Differently cased JSON keys must not create conflicting proofs
				// for the same case-insensitive expression reference.
				return false
			}
			names[key] = true
		}
		allSafe := scriptLiteralWord
		for name, property := range value {
			paths := map[string]scriptValueProof{}
			if !matrixScriptLiteralPaths("", property, paths) {
				return false
			}
			allSafe = min(allSafe, paths[""])
			if scriptPathName(name) {
				copyScriptPaths(path+"."+strings.ToLower(name), paths, safe)
			}
		}
		safe[path+"[*]"] = allSafe
	case []any:
		safe[path] = scriptUnsafe
		allSafe := scriptLiteralWord
		for index, element := range value {
			child := path + "[" + strconv.Itoa(index) + "]"
			if !matrixScriptLiteralPaths(child, element, safe) {
				return false
			}
			allSafe = min(allSafe, safe[child])
		}
		safe[path+"[*]"] = allSafe
	}
	return true
}

func copyScriptPaths(prefix string, paths, destination map[string]scriptValueProof) {
	for suffix, safe := range paths {
		destination[prefix+suffix] = safe
	}
}

func safeMatrixScriptReference(node exprlang.Node, proofs map[string]scriptValueProof, argument bool) bool {
	path := matrixScriptReferencePath(node)
	if path != "" && proofs[path].safe(argument) {
		return true
	}
	if index, ok := node.(*exprlang.IndexAccessNode); ok {
		base := matrixScriptReferencePath(index.Operand)
		return base != "" && proofs[base+"[*]"].safe(argument)
	}
	return false
}

func matrixScriptReferencePath(node exprlang.Node) string {
	switch node := node.(type) {
	case *exprlang.VariableNode:
		if strings.EqualFold(node.Name, "matrix") {
			return "matrix"
		}
	case *exprlang.ObjectDerefNode:
		if base := matrixScriptReferencePath(node.Receiver); base != "" && scriptPathName(node.Property) {
			return base + "." + strings.ToLower(node.Property)
		}
	case *exprlang.IndexAccessNode:
		base := matrixScriptReferencePath(node.Operand)
		if base == "" {
			return ""
		}
		switch index := node.Index.(type) {
		case *exprlang.IntNode:
			return base + "[" + strconv.Itoa(index.Value) + "]"
		case *exprlang.StringNode:
			if scriptPathName(index.Value) {
				return base + "." + strings.ToLower(index.Value)
			}
		}
	}
	return ""
}

// Only unambiguous dotted identifier paths participate in these proofs.
func scriptPathName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

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
	if rule.scriptEnv == nil {
		return false
	}
	mapping := workflowlang.EnvMappingView(rule.scriptEnv)
	names, complete := workflowlang.InputResolver.Names(mapping)
	if !complete {
		return false
	}
	if _, present := names[strings.ToLower(name)]; !present {
		return false
	}
	value, known := workflowlang.InputResolver.Value(mapping, name)
	return known && scriptLiteralProof(value).safe(argument)
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
	type pathProof struct {
		proof scriptValueProof
		count int
	}
	type axisProofs struct {
		baseAlternatives    int
		includeAlternatives int
		paths               map[string]pathProof
	}
	axes := map[string]*axisProofs{}
	add := func(name string, value workflowlang.RawYAMLValue, expressions, include bool) bool {
		if !scriptPathName(name) {
			return true
		}
		paths := map[string]scriptValueProof{}
		if !matrixScriptValuePaths("matrix."+name, value, expressions, paths) {
			return false
		}
		axis := axes[name]
		if axis == nil {
			axis = &axisProofs{paths: map[string]pathProof{}}
			axes[name] = axis
		}
		if include {
			axis.includeAlternatives++
		} else {
			axis.baseAlternatives++
		}
		for path, proof := range paths {
			previous, exists := axis.paths[path]
			if exists {
				proof = min(proof, previous.proof)
				previous.count++
			} else {
				previous.count = 1
			}
			previous.proof = proof
			axis.paths[path] = previous
		}
		return true
	}
	includeSelectsBase := false
	for name, row := range resolved.Rows {
		elements, complete := matrixcheck.RowElements(row, true)
		if !complete {
			return nil
		}
		for _, element := range elements {
			if !add(name, element.Value, element.Expressions, false) {
				return nil
			}
		}
	}
	if includes != nil {
		for _, combination := range includes.Combinations {
			for key, assignment := range combination.Assigns {
				name, known := matrixcheck.RowName(key, &workflowlang.MatrixRow{Name: assignment.Key, Evaluated: combination.Evaluated})
				if !known || !add(name, assignment.Value, !combination.Evaluated, true) {
					return nil
				}
				if _, baseAxis := resolved.Rows[name]; baseAxis {
					includeSelectsBase = true
				}
			}
		}
	}
	for _, axis := range axes {
		alternatives := axis.baseAlternatives + axis.includeAlternatives
		includeCoverage := true
		if axis.baseAlternatives == 0 {
			// Include-only fields are proved only when every include supplies them
			// without selecting a subset of the declared base axes.
			alternatives = len(includes.Combinations)
			includeCoverage = alternatives > 0 && !includeSelectsBase && axis.includeAlternatives == alternatives
		}
		for path, proof := range axis.paths {
			if proof.count < alternatives || !includeCoverage {
				// A missing member, index, or include-only assignment can be empty.
				proof.proof = min(proof.proof, scriptArgumentOnly)
			}
			axis.paths[path] = proof
		}
	}
	result := map[string]scriptValueProof{}
	for _, axis := range axes {
		for path, proof := range axis.paths {
			if proof.proof != scriptUnsafe {
				result[path] = proof.proof
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
		// A wildcard can select no value or a missing member, so it is only
		// proved safe when the caller also proves shell argument position.
		safe[path+"[*]"] = min(allSafe, scriptArgumentOnly)
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
		// A wildcard can select no value or a missing member, so it is only
		// proved safe when the caller also proves shell argument position.
		safe[path+"[*]"] = min(allSafe, scriptArgumentOnly)
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
		// A wildcard can select no value or a missing member, so it is only
		// proved safe when the caller also proves shell argument position.
		safe[path+"[*]"] = min(allSafe, scriptArgumentOnly)
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
		// A wildcard can select no value or a missing member, so it is only
		// proved safe when the caller also proves shell argument position.
		safe[path+"[*]"] = min(allSafe, scriptArgumentOnly)
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
	if path != "" {
		if proof, exists := proofs[path]; exists {
			return proof.safe(argument)
		}
	}
	if !argument {
		return false
	}
	base := ""
	switch node := node.(type) {
	case *exprlang.IndexAccessNode:
		base = matrixScriptReferencePath(node.Operand)
	case *exprlang.ObjectDerefNode:
		base = matrixScriptReferencePath(node.Receiver)
	}
	return base != "" && proofs[base+"[*]"].safe(true)
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

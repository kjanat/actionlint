package expressions

import (
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
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
	if !rule.scriptEnv.Present() {
		return false
	}
	semantic := rule.scriptEnv.Mapping().Lookup(name)
	text, known := semantic.ScalarString()
	return known && scriptLiteralProof(text).safe(argument)
}

func (rule *Checker) matrixResolution(strategy workflowdecl.StrategyView) matrixcheck.Resolution {
	if !strategy.Present() {
		return matrixcheck.Resolution{}
	}
	return strategy.MatrixResolution()
}

// safeMatrixScriptReferences proves individual paths across all declared axis
// values and include assignments, without expanding the Cartesian product.
// Includes apply after exclusions, so an excluded match may become a sparse job.
func safeMatrixScriptReferences(resolution matrixcheck.Resolution) map[string]scriptValueProof {
	result := map[string]scriptValueProof{}
	for name := range resolution.ReachableNames() {
		if !scriptPathName(name) {
			continue
		}
		reachable := resolution.Reachable(name)
		if !reachable.Complete {
			return nil
		}
		paths := map[string]scriptValueProof{}
		counts := map[string]int{}
		for _, candidate := range reachable.Values {
			current := map[string]scriptValueProof{}
			if !matrixScriptValuePaths("matrix."+name, candidate, current) {
				return nil
			}
			for path, proof := range current {
				if previous, exists := paths[path]; exists {
					paths[path] = min(previous, proof)
				} else {
					paths[path] = proof
				}
				counts[path]++
			}
		}
		for path, proof := range paths {
			if counts[path] != len(reachable.Values) || reachable.MayBeMissing {
				proof = min(proof, scriptArgumentOnly)
			}
			if proof != scriptUnsafe {
				result[path] = proof
			}
		}
	}
	return result
}

func matrixScriptValuePaths(path string, item value.Value, safe map[string]scriptValueProof) bool {
	if item.State() == value.Dynamic || item.State() == value.Invalid || item.State() == value.Missing {
		return false
	}
	switch item.Kind() {
	case value.String:
		text, ok := item.Text()
		if !ok {
			return false
		}
		safe[path] = scriptLiteralProof(text)
	case value.Number, value.Bool, value.Null:
		safe[path] = scriptArgumentOnly
	case value.Object:
		safe[path] = scriptUnsafe
		allSafe := scriptLiteralWord
		names := map[string]bool{}
		for name, property := range item.Props() {
			key := strings.ToLower(name)
			if names[key] {
				return false
			}
			names[key] = true
			paths := map[string]scriptValueProof{}
			if !matrixScriptValuePaths("", property, paths) {
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
	case value.Array:
		safe[path] = scriptUnsafe
		allSafe := scriptLiteralWord
		for index := range item.Len() {
			child := path + "[" + strconv.Itoa(index) + "]"
			element, _ := item.Index(index)
			if !matrixScriptValuePaths(child, element, safe) {
				return false
			}
			allSafe = min(allSafe, safe[child])
		}
		// A wildcard can select no value or a missing member, so it is only
		// proved safe when the caller also proves shell argument position.
		safe[path+"[*]"] = min(allSafe, scriptArgumentOnly)
	case value.KindUnknown:
		return false
	default:
		return false
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

// Package dependencypins supplies the native reference check for component comparisons.
package dependencypins

import (
	"context"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Checker requires remote actions and reusable workflows to use full commit
// hashes. Local dependencies and Docker image references follow other policies.
type Checker struct{}

func (Checker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{
		Name:        "pinned-actions",
		Description: "Pin remote dependencies to full commit hashes",
		Category:    "security", RequiredInputs: []checkinputs.InputKind{checkinputs.Dependencies},
	}
}

func (Checker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, dependency := range input.Dependencies {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		kind := "action"
		switch dependency.Kind {
		case "action":
		case "workflow":
			kind = "reusable workflow"
		default:
			continue
		}
		if strings.HasPrefix(dependency.Uses, "./") || strings.HasPrefix(dependency.Uses, "docker://") {
			continue
		}
		index := strings.LastIndexByte(dependency.Uses, '@')
		if index < 0 { // Malformed references belong to syntax validation.
			continue
		}
		if pinned(dependency.Uses[index+1:]) {
			continue
		}
		reference := dependency.Reference
		findings = append(findings, results.Diagnostic{
			Reference: &reference,
			Rule:      "pinned-actions",
			Code:      "unpinned-dependency",
			Severity:  "warning",
			Message:   kind + " must be pinned to a full commit hash",
			Path:      input.Path,
			Start:     dependency.Start,
			End:       dependency.End,
		})
	}
	return findings, nil
}

func pinned(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

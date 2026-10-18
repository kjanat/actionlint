// Package pinnedactions demonstrates a native analyzer using shared dependency
// facts. Its Rust component counterpart is ../pinned-actions.rs. Neither parses
// source content or depends on the host's private syntax tree.
package pinnedactions

import (
	"context"
	"strings"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
)

// Analyzer requires remote actions and reusable workflows to use full commit
// hashes. Local dependencies and Docker image references follow other policies.
type Analyzer struct{}

func (Analyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{
		Name:        "pinned-actions",
		Description: "Pin remote dependencies to full commit hashes",
		Requires:    []analyzer.Fact{analyzer.Dependencies},
	}
}

func (Analyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
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
		findings = append(findings, diagnostic.Diagnostic{
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

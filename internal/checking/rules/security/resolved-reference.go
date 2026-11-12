package security

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

// ResolvedReferenceCheck reports commit pins from captured resolver observations.
// It does not resolve references or acquire repository data.
type ResolvedReferenceCheck struct{}

func NewResolvedReferenceCheck() *ResolvedReferenceCheck {
	return &ResolvedReferenceCheck{}
}

func (*ResolvedReferenceCheck) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "remote-resolution", Description: "Report available commit pins from resolved action and workflow references", Category: "security", Subcategory: "dependencies", Provider: "resolver", Maturity: "stable", Help: "Review the provider's resolved commit before pinning this dependency.", RequiredInputs: []rulemeta.InputKind{rulemeta.Dependencies}}
}

func (*ResolvedReferenceCheck) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, result := range input.ResolvedDependencies {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		dep := result.Dependency
		name := strings.TrimSuffix(dep.Uses, "@"+result.Ref)
		if result.Commit == "" || result.Ref == result.Commit {
			continue
		}
		reference := dep.Reference
		message := fmt.Sprintf("%s resolves to commit %s (observed %s)", dep.Uses, result.Commit, result.ObservedAt)
		if id, known := result.RepositoryID.Value(); known {
			message = fmt.Sprintf("%s resolves to commit %s (repository ID %d, observed %s)", dep.Uses, result.Commit, id, result.ObservedAt)
		}
		d := results.Diagnostic{Rule: "remote-resolution", Code: "pin-available", Severity: "info", Reference: &reference, Path: input.Path, Start: dep.Start, End: dep.End, Message: message}
		if dep.End != dep.Start {
			d.Fixes = []results.Fix{{Description: "Pin " + name + " to resolved commit", Applicability: results.FixSuggested, Revisions: map[string]string{input.Path: input.Reference.Revision}, Edits: []results.Edit{{Path: input.Path, Start: dep.Start, End: dep.End, Replacement: strconv.Quote(name + "@" + result.Commit)}}}}
		}
		findings = append(findings, d)
	}
	return findings, nil
}

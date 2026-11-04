package checks

import (
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// VendorLockIn reports repository URLs that GitHub cannot resolve in uses.
func VendorLockIn(uses declaration.StringView, target string) []Finding {
	if target != "github" || !uses.Present() || uses.ContainsExpression() {
		return nil
	}
	ref, present := references.ParseRepositoryURL(uses.Value())
	if !present {
		return nil
	}
	finding := Finding{
		Rule: "vendor-lock-in", Severity: "error", Start: position(viewPosition(uses.Pos())),
		Message: fmt.Sprintf("GitHub Actions requires owner/repo[/path]@ref in uses, not a full repository URL (host %q)", ref.Host()),
	}
	if ref.Shorthand() != "" {
		finding.Message = fmt.Sprintf("GitHub Actions requires repository shorthand in uses; write %q instead of the full GitHub URL", ref.Shorthand())
		if source := uses.ScalarSource(); source.Present() {
			start, startOK := source.Position(1, 1)
			end, endOK := source.EndPos(1, ref.PrefixLength()+1)
			if startOK && endOK {
				finding.Fixes = []diagnostic.Fix{{
					Description: "Remove the GitHub URL prefix", Applicability: diagnostic.FixSafe,
					Edits: []diagnostic.Edit{{Start: position(&start), End: position(&end)}},
				}}
			}
		}
	}
	return []Finding{finding}
}

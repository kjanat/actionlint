package policy

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/sha256hex"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestSuppressionUseAcrossInvocations(t *testing.T) {
	t.Parallel()
	ref := sourcetext.Reference{Document: "shared-action", Revision: "revision-one"}
	position := sourcetext.Position{Line: 4, Column: 15}
	var usage suppressionUses
	usage.observe(ref, position, "shellcheck")
	candidate := func(document, revision, rule string) *finding.Finding {
		return &finding.Finding{
			Rule: "unused-suppression", Start: position,
			Reference: &sourcetext.Reference{Document: document, Revision: revision}, UnusedSuppressionRule: rule,
		}
	}
	files := []evaluatedBatch{
		{Findings: []*finding.Finding{candidate("shared-action", "revision-one", "shellcheck")}},
		{Findings: []*finding.Finding{
			candidate("shared-action", "revision-one", "expression-reference"),
			candidate("other-action", "revision-one", "shellcheck"),
			candidate("shared-action", "revision-two", "shellcheck"),
		}},
	}
	usage.discardUnproven(files, nil)
	if len(files[0].Findings) != 0 || len(files[1].Findings) != 3 {
		t.Fatalf("usage crossed selector, document or revision boundaries: %+v", files)
	}
}

func TestUnusedFixKeepsSelectorsUsedByOtherInvocations(t *testing.T) {
	t.Parallel()
	const content = "run: echo ok # ignore shellcheck expression-reference -- keep reason\n"
	position := sourcetext.Position{Line: 1, Column: strings.Index(content, "#") + 1}
	ref := sourcetext.Reference{Document: "physical-action.yml", Revision: sha256hex.Digest([]byte(content))}
	var usage suppressionUses
	usage.observe(ref, position, "shellcheck")
	files := []evaluatedBatch{{Policy: Policy{Content: []byte(content), AdditionalRules: []string{"shellcheck", "expression-reference"}}}}
	for _, name := range []string{"shellcheck", "expression-reference"} {
		files[0].Findings = append(files[0].Findings, &finding.Finding{
			Rule: "unused-suppression", Start: position, Reference: &ref, Path: "alias/action.yml", UnusedSuppressionRule: name,
		})
	}
	usage.discardUnproven(files, nil)
	addUnusedSuppressionFixes(files)
	if len(files[0].Findings) != 1 || len(files[0].Findings[0].Fixes) != 1 {
		t.Fatalf("lost reconciled cleanup: %+v", files)
	}
	fix := files[0].Findings[0].Fixes[0]
	if fix.Applicability != results.FixSafe || len(fix.Edits) != 1 || fix.Edits[0].Replacement != "# ignore shellcheck -- keep reason" || fix.Edits[0].Path != "alias/action.yml" || fix.Revisions["alias/action.yml"] != ref.Revision {
		t.Fatalf("cleanup removed a selector used by another invocation: %+v", fix)
	}
}

func TestIncompleteSuppressionInvocationInvalidatesOnlyItsRevision(t *testing.T) {
	t.Parallel()
	ref := sourcetext.Reference{Document: "shared-action", Revision: "revision-one"}
	var usage suppressionUses
	checks := []report.IncompleteCheck{{Source: ref}}
	candidate := func(document, revision string) *finding.Finding {
		return &finding.Finding{
			Rule: "unused-suppression", Reference: &sourcetext.Reference{Document: document, Revision: revision}, UnusedSuppressionRule: "shellcheck",
		}
	}
	files := []evaluatedBatch{
		{Findings: []*finding.Finding{candidate("shared-action", "revision-one")}},
		{Findings: []*finding.Finding{
			candidate("shared-action", "revision-one"),
			candidate("other-action", "revision-one"),
			candidate("shared-action", "revision-two"),
		}},
	}
	usage.discardUnproven(files, checks)
	if len(files[0].Findings) != 0 || len(files[1].Findings) != 2 || files[1].Findings[0].Reference.Document != "other-action" || files[1].Findings[1].Reference.Revision != "revision-two" {
		t.Fatalf("incomplete invocation leaked cleanup or crossed source revisions: %+v", files)
	}
}

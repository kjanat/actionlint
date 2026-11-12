package checking

import (
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestSuppressionUseAcrossInvocations(t *testing.T) {
	t.Parallel()
	ref := sourcepos.Reference{Document: "shared-action", Revision: "revision-one"}
	position := sourcepos.Position{Line: 4, Column: 15}
	var usage suppressionUses
	usage.observe(ref, position, "shellcheck")
	candidate := func(document, revision, rule string) *findingmodel.Finding {
		return &findingmodel.Finding{
			Rule: "unused-suppression", Start: position,
			Reference: &sourcepos.Reference{Document: document, Revision: revision}, UnusedSuppressionRule: rule,
		}
	}
	files := []analyzedFile{
		{errors: []*findingmodel.Finding{candidate("shared-action", "revision-one", "shellcheck")}},
		{errors: []*findingmodel.Finding{
			candidate("shared-action", "revision-one", "expression-reference"),
			candidate("other-action", "revision-one", "shellcheck"),
			candidate("shared-action", "revision-two", "shellcheck"),
		}},
	}
	usage.discardUnproven(files, nil)
	if len(files[0].errors) != 0 || len(files[1].errors) != 3 {
		t.Fatalf("usage crossed selector, document or revision boundaries: %+v", files)
	}
}

func TestUnusedFixKeepsSelectorsUsedByOtherInvocations(t *testing.T) {
	t.Parallel()
	const content = "run: echo ok # ignore shellcheck expression-reference -- keep reason\n"
	position := sourcepos.Position{Line: 1, Column: strings.Index(content, "#") + 1}
	ref := sourcepos.Reference{Document: "physical-action.yml", Revision: sourceid.Digest([]byte(content))}
	var usage suppressionUses
	usage.observe(ref, position, "shellcheck")
	files := []analyzedFile{{source: sourceUnit{Content: []byte(content)}}}
	for _, name := range []string{"shellcheck", "expression-reference"} {
		files[0].errors = append(files[0].errors, &findingmodel.Finding{
			Rule: "unused-suppression", Start: position, Reference: &ref, Path: "alias/action.yml", UnusedSuppressionRule: name,
		})
	}
	usage.discardUnproven(files, nil)
	addUnusedSuppressionFixes(files, []string{"shellcheck", "expression-reference"})
	if len(files[0].errors) != 1 || len(files[0].errors[0].Fixes) != 1 {
		t.Fatalf("lost reconciled cleanup: %+v", files)
	}
	fix := files[0].errors[0].Fixes[0]
	if fix.Applicability != results.FixSafe || len(fix.Edits) != 1 || fix.Edits[0].Replacement != "# ignore shellcheck -- keep reason" || fix.Edits[0].Path != "alias/action.yml" || fix.Revisions["alias/action.yml"] != ref.Revision {
		t.Fatalf("cleanup removed a selector used by another invocation: %+v", fix)
	}
}

func TestIncompleteSuppressionInvocationInvalidatesOnlyItsRevision(t *testing.T) {
	t.Parallel()
	ref := sourcepos.Reference{Document: "shared-action", Revision: "revision-one"}
	var usage suppressionUses
	var completion checkCompletion
	completion.markIncomplete(ref)
	candidate := func(document, revision string) *findingmodel.Finding {
		return &findingmodel.Finding{
			Rule: "unused-suppression", Reference: &sourcepos.Reference{Document: document, Revision: revision}, UnusedSuppressionRule: "shellcheck",
		}
	}
	files := []analyzedFile{
		{errors: []*findingmodel.Finding{candidate("shared-action", "revision-one")}},
		{errors: []*findingmodel.Finding{
			candidate("shared-action", "revision-one"),
			candidate("other-action", "revision-one"),
			candidate("shared-action", "revision-two"),
		}},
	}
	usage.discardUnproven(files, completion.incompleteChecks())
	if len(files[0].errors) != 0 || len(files[1].errors) != 2 || files[1].errors[0].Reference.Document != "other-action" || files[1].errors[1].Reference.Revision != "revision-two" {
		t.Fatalf("incomplete invocation leaked cleanup or crossed source revisions: %+v", files)
	}
}

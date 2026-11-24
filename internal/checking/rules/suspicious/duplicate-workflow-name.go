package suspicious

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/nameindex"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// duplicateWorkflowNameRuleName must equal DuplicateWorkflowNameRule.Name,
// which stays a literal so the registration test can find the definition.
const duplicateWorkflowNameRuleName = "duplicate-workflow-name"

var DuplicateWorkflowNameRule = &define.Rule{
	Name:         "duplicate-workflow-name",
	Description:  "Report workflows whose name another workflow in the repository also uses",
	Category:     "suspicious",
	Subcategory:  "workflows",
	Recommended:  true,
	DefaultLevel: "warn",
	Explanation: "Report workflows under .github/workflows that share a top-level name, ignoring case. " +
		"A workflow_run filter that names one of them also matches the others.",
	Help:    "Give each workflow its own name.",
	Prepare: prepareDuplicateWorkflowName,
	Enabled: define.RuleInputs.InProjectWorkflows,
	Checks: func(c define.RuleInputs) define.Checks {
		return define.Checks{Workflow: func(w workflows.BoundWorkflow) []reporting.StepFinding {
			return DuplicateWorkflowName(c.WorkflowNameInventory, w)
		}}
	},
}

// DuplicateWorkflowName requires others to exclude the workflow itself.
func DuplicateWorkflowName(others names.Inventory, w workflows.BoundWorkflow) []reporting.StepFinding {
	if !w.Present() {
		return nil
	}
	name, literal := names.Declare(w.Name).Literal()
	if !literal {
		return nil
	}
	matches := others.SharingName(name)
	if len(matches) == 0 {
		return nil
	}
	described := make([]string, len(matches))
	for i, other := range matches {
		described[i] = ".github/workflows/" + filepath.Base(other.Path)
		if other.Name != name {
			described[i] += fmt.Sprintf(" (as %q)", other.Name)
		}
	}
	slices.Sort(described)
	return []reporting.StepFinding{reporting.Warning(duplicateWorkflowNameRuleName, "", w.Name.Pos,
		"workflow name %q is also used by %s; the Actions tab cannot tell these workflows apart",
		name, strings.Join(described, ", "))}
}

func prepareDuplicateWorkflowName(c define.RuleInputs) (define.InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, duplicateWorkflowNameRuleName) == "off" {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	workflow, available := c.SourceSnapshot.WorkflowView()
	if !available || !workflow.Present() {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	if _, literal := names.Declare(workflow.Name).Literal(); !literal {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	inventory, err := c.ProjectWorkflowNames()
	if err != nil {
		return nil, err
	}
	self := c.DocumentPath()
	c.WorkflowNameInventory = inventory.ExcludingLiteral(func(path string) bool {
		return nameindex.SamePath(path, self)
	})
	return define.PreparedInputs{RuleInputs: c}, nil
}

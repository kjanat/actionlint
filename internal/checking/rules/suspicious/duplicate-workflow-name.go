package suspicious

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/files/names"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// duplicateWorkflowNameRuleName must equal DuplicateWorkflowNameRule.Name,
// which stays a literal so the registration test can find the definition.
const duplicateWorkflowNameRuleName = "duplicate-workflow-name"

// DuplicateWorkflowNameRule is the single definition of the duplicate-workflow-name rule.
var DuplicateWorkflowNameRule = &define.Rule{
	Name:         "duplicate-workflow-name",
	Description:  "Report workflows whose name another workflow in the repository also uses",
	Category:     "suspicious",
	Subcategory:  "workflows",
	Recommended:  true,
	DefaultLevel: "warn",
	Explanation: "Report a workflow whose top-level name another workflow under .github/workflows also uses, ignoring case. " +
		"GitHub accepts the files, but the Actions tab lists the workflows under names that cannot be told apart, " +
		"and a workflow_run filter naming one of them also matches the others spelled the same way. Workflows without a name " +
		"are listed by their path and are not reported. Names written as expressions are not compared.",
	Help:    "Give each workflow its own name.",
	Prepare: prepareDuplicateWorkflowName,
	Enabled: define.RuleInputs.InProjectWorkflows,
	Checks: func(c define.RuleInputs) define.Checks {
		return define.Checks{Workflow: func(w workflows.Workflow) []reporting.StepFinding {
			return DuplicateWorkflowName(c.WorkflowNameInventory, w)
		}}
	},
}

// DuplicateWorkflowName reports the workflow's plain-text name when another
// workflow in others uses it too. others must not contain the workflow itself.
// It does not read files.
func DuplicateWorkflowName(others namemodel.Inventory, w workflows.Workflow) []reporting.StepFinding {
	if !w.Present() {
		return nil
	}
	name, literal := namemodel.Declare(w.Name).Literal()
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

// prepareDuplicateWorkflowName loads the project's workflow names without the
// checked workflow itself. An incomplete inventory still proves the duplicates
// among the workflows it could read.
func prepareDuplicateWorkflowName(c define.RuleInputs) (define.InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, duplicateWorkflowNameRuleName) == "off" {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	workflow, available := c.SourceSnapshot.WorkflowView()
	if !available || !workflow.Present() {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	if _, literal := namemodel.Declare(workflow.Name).Literal(); !literal {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	inventory, err := c.ProjectWorkflowNames()
	if err != nil {
		return nil, err
	}
	self := c.DocumentPath()
	c.WorkflowNameInventory = inventory.ExcludingLiteral(func(path string) bool {
		return names.SamePath(path, self)
	})
	return define.PreparedInputs{RuleInputs: c}, nil
}

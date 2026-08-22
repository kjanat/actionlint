package actionlint

import (
	"fmt"
	"strconv"
	"strings"
)

// RuleWorkflowCall is a rule checker to check workflow call at jobs.<job_id>.
type RuleWorkflowCall struct {
	RuleBase
	workflowCallEventPos *Pos
	workflowPath         string
	cache                *LocalReusableWorkflowCache
}

// NewRuleWorkflowCall creates a new RuleWorkflowCall instance. 'workflowPath' is a file path to
// the workflow which is relative to a project root directory or an absolute path.
func NewRuleWorkflowCall(workflowPath string, cache *LocalReusableWorkflowCache) *RuleWorkflowCall {
	return &RuleWorkflowCall{
		RuleBase: RuleBase{
			name: "workflow-call",
			desc: "Checks for reusable workflow calls. Inputs and outputs of called reusable workflow are checked",
		},
		workflowCallEventPos: nil,
		workflowPath:         workflowPath,
		cache:                cache,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleWorkflowCall) VisitWorkflowPre(n *Workflow) error {
	for _, e := range n.On {
		if e, ok := e.(*WorkflowCallEvent); ok {
			rule.workflowCallEventPos = e.Pos
			// Register this reusable workflow in cache so that it does not need to parse this workflow
			// file again when this workflow is called by other workflows.
			rule.cache.WriteWorkflowCallEvent(rule.workflowPath, e)
			break
		}
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleWorkflowCall) VisitJobPre(n *Job) error {
	if n.WorkflowCall == nil {
		return nil
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return nil
	}

	if local, ok := workflowCallUsesLocalSpec(u.Value); ok {
		rule.checkWorkflowCallUsesLocal(n.WorkflowCall, local)
		return nil
	}

	if _, ok := workflowCallUsesRepoRef(u.Value); ok {
		return nil
	}

	if strings.HasPrefix(u.Value, "./") || strings.HasPrefix(u.Value, "$/") {
		// When the specification is invalid and it is local reusable workflow call, remember it caused
		// an error by setting `nil` to cache. This can prevent redundant 'could not read workflow call'
		// error.
		rule.cache.writeCache(u.Value, nil)
	}

	rule.Errorf(
		u.Pos,
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value,
	)
	return nil
}

func (rule *RuleWorkflowCall) checkWorkflowCallUsesLocal(call *WorkflowCall, localSpec string) {
	u := call.Uses
	m, err := rule.cache.FindMetadata(localSpec)
	if err != nil {
		msg := strings.Replace(err.Error(), strconv.Quote(localSpec), strconv.Quote(u.Value), 1)
		rule.Error(u.Pos, msg)
		return
	}
	if m == nil {
		rule.Debug("Skip workflow call %q since no metadata was found", u.Value)
		return
	}

	// Validate inputs
	for n, i := range m.Inputs {
		if i != nil && i.Required {
			if _, ok := call.Inputs[n]; !ok {
				rule.Errorf(u.Pos, "input %q is required by %q reusable workflow", i.Name, u.Value)
			}
		}
	}
	for n, i := range call.Inputs {
		if _, ok := m.Inputs[n]; !ok {
			note := "no input is defined"
			if len(m.Inputs) > 0 {
				is := make([]string, 0, len(m.Inputs))
				for _, i := range m.Inputs {
					is = append(is, i.Name)
				}
				if len(is) == 1 {
					note = fmt.Sprintf("defined input is %q", is[0])
				} else {
					note = "defined inputs are " + sortedQuotes(is)
				}
			}
			rule.Errorf(i.Name.Pos, "input %q is not defined in %q reusable workflow. %s", i.Name.Value, u.Value, note)
		}
	}

	// Validate secrets
	if !call.InheritSecrets {
		for n, s := range m.Secrets {
			if s.Required {
				if _, ok := call.Secrets[n]; !ok {
					rule.Errorf(u.Pos, "secret %q is required by %q reusable workflow", s.Name, u.Value)
				}
			}
		}
		for n, s := range call.Secrets {
			if _, ok := m.Secrets[n]; !ok {
				note := "no secret is defined"
				if len(m.Secrets) > 0 {
					ss := make([]string, 0, len(m.Secrets))
					for _, s := range m.Secrets {
						ss = append(ss, s.Name)
					}
					if len(ss) == 1 {
						note = fmt.Sprintf("defined secret is %q", ss[0])
					} else {
						note = "defined secrets are " + sortedQuotes(ss)
					}
				}
				rule.Errorf(s.Name.Pos, "secret %q is not defined in %q reusable workflow. %s", s.Name.Value, u.Value, note)
			}
		}
	}

	rule.Debug("Validated reusable workflow %q", u.Value)
}

// Normalize a local or self-repository reusable workflow reference to the existing local cache key.
func workflowCallUsesLocalSpec(u string) (string, bool) {
	if strings.HasPrefix(u, "$/") {
		s, ok := selfRepositoryUsesLocalSpec(u)
		if !ok {
			return "", false
		}
		u = s
	} else if !strings.HasPrefix(u, "./") {
		return "", false
	}
	path := strings.TrimPrefix(u, "./")

	// Cannot contain a ref
	idx := strings.IndexRune(path, '@')
	if idx > 0 {
		return "", false
	}

	return u, len(path) > 0
}

// Parse {owner}/{repo}/{path to workflow.yml}@{ref} and return the {ref} part. The second return
// value is false when the specification has another form.
// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#calling-a-reusable-workflow
func workflowCallUsesRepoRef(u string) (string, bool) {
	// Repo reference must start with owner
	if strings.HasPrefix(u, ".") || strings.HasPrefix(u, "$") {
		return "", false
	}

	idx := strings.IndexRune(u, '/')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat owner

	idx = strings.IndexRune(u, '/')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat repo

	idx = strings.IndexRune(u, '@')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat workflow path

	if len(u) == 0 {
		return "", false
	}
	return u, true
}

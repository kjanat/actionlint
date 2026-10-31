package calls

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func callFinding(pos workflowlang.Pos, format string, args ...any) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{Rule: "workflow-call", Message: fmt.Sprintf(format, args...), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}}
}

// CheckArguments validates required and supplied inputs and secrets against
// reusable workflow metadata.
func CheckArguments(call declaration.WorkflowCallView, m *Metadata) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	if !call.Present() || !call.Uses().Present() || m == nil {
		return findings
	}
	u := call.Uses()
	report := func(pos workflowlang.Pos, format string, args ...any) {
		findings = append(findings, callFinding(pos, format, args...))
	}
	// Validate inputs
	for n, i := range m.Inputs {
		if i.Required {
			if _, ok := call.InputsAt(n); !ok {
				report(callPosition(u), "input %q is required by %q reusable workflow", i.Name, u.Value())
			}
		}
	}
	for n, i := range call.Inputs() {
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
					note = "defined inputs are " + stringutil.SortedQuotes(is)
				}
			}
			report(callPosition(i.Name()), "input %q is not defined in %q reusable workflow. %s", i.Name().Value(), u.Value(), note)
		}
	}

	// Validate secrets
	if !call.InheritSecrets() {
		for n, s := range m.Secrets {
			if s.Required {
				if _, ok := call.SecretsAt(n); !ok {
					report(callPosition(u), "secret %q is required by %q reusable workflow", s.Name, u.Value())
				}
			}
		}
		for n, s := range call.Secrets() {
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
						note = "defined secrets are " + stringutil.SortedQuotes(ss)
					}
				}
				report(callPosition(s.Name()), "secret %q is not defined in %q reusable workflow. %s", s.Name().Value(), u.Value(), note)
			}
		}
	}

	return findings
}

// CheckPermissions compares every callee job's requirements with the explicit
// caller grant. Conditions do not weaken this contract: GitHub validates it before execution.
func CheckPermissions(call declaration.WorkflowCallView, granted ResolvedPermissions, m *Metadata) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	if !call.Present() || !call.Uses().Present() || m == nil || granted.Kind != PermissionsDeclared {
		return findings
	}
	report := func(pos workflowlang.Pos, format string, args ...any) {
		findings = append(findings, callFinding(pos, format, args...))
	}
	ids := make([]string, 0, len(m.JobPermissions))
	for id := range m.JobPermissions {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		required := m.JobPermissions[id]
		ss := make([]string, 0, len(required))
		for s := range required {
			ss = append(ss, s)
		}
		slices.Sort(ss)

		want, have := []string{}, []string{}
		for _, s := range ss {
			if granted.Levels[s] >= required[s] {
				continue
			}
			want = append(want, s+": "+required[s].String())
			have = append(have, s+": "+granted.Levels[s].String())
		}
		if len(want) == 0 {
			continue
		}

		report(
			callPosition(call.Uses()),
			"nested job %q of %q requires %s but the calling job grants %s",
			id,
			call.Uses().Value(),
			stringutil.Quotes(want),
			stringutil.Quotes(have),
		)
	}

	return findings
}

func callPosition(node declaration.StringView) workflowlang.Pos { pos, _ := node.Pos(); return pos }

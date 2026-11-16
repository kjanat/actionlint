// Package workflowcall checks reusable-workflow arguments, secrets and permission grants.
package workflowcall

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

func callFinding(pos source.Position, format string, args ...any) results.Diagnostic {
	return results.Diagnostic{Message: fmt.Sprintf(format, args...), Start: pos}
}

// CheckArguments validates required and supplied inputs and secrets against
// reusable workflow metadata.
func CheckArguments(call *definition.WorkflowCall, m *calls.Metadata) []results.Diagnostic {
	var findings []results.Diagnostic
	if call == nil || call.Uses == nil || m == nil {
		return findings
	}
	u := call.Uses
	report := func(pos source.Position, format string, args ...any) {
		findings = append(findings, callFinding(pos, format, args...))
	}
	// Validate inputs
	for _, i := range m.Inputs.MissingRequired(func(id string) bool { _, ok := call.Inputs[id]; return ok }) {
		report(callPosition(u), "input %q is required by %q reusable workflow", i.Name, u.Value)
	}
	for n, i := range call.Inputs {
		if _, ok := m.Inputs[n]; !ok {
			report(callPosition(i.Name), "input %q is not defined in %q reusable workflow. %s", i.Name.Text(), u.Value, declaredNote("input", m.Inputs.Names()))
		}
	}

	// Validate secrets
	if !call.InheritSecrets {
		for _, s := range m.Secrets.MissingRequired(func(id string) bool { _, ok := call.Secrets[id]; return ok }) {
			report(callPosition(u), "secret %q is required by %q reusable workflow", s.Name, u.Value)
		}
		for n, s := range call.Secrets {
			if _, ok := m.Secrets[n]; !ok {
				report(callPosition(s.Name), "secret %q is not defined in %q reusable workflow. %s", s.Name.Text(), u.Value, declaredNote("secret", m.Secrets.Names()))
			}
		}
	}

	return findings
}

// declaredNote lists the names a reusable workflow declares for kind, which is
// "input" or "secret", after a call supplies an undeclared one.
func declaredNote(kind string, names []string) string {
	switch len(names) {
	case 0:
		return fmt.Sprintf("no %s is defined", kind)
	case 1:
		return fmt.Sprintf("defined %s is %q", kind, names[0])
	default:
		return fmt.Sprintf("defined %ss are %s", kind, messages.SortedQuotes(names))
	}
}

// CheckPermissions compares every callee job's requirements with the explicit
// caller grant. Conditions do not weaken this contract: GitHub validates it before execution.
func CheckPermissions(call *definition.WorkflowCall, granted calls.ResolvedPermissions, m *calls.Metadata) []results.Diagnostic {
	var findings []results.Diagnostic
	if call == nil || call.Uses == nil || m == nil || granted.Kind != calls.PermissionsDeclared {
		return findings
	}
	report := func(pos source.Position, format string, args ...any) {
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
			callPosition(call.Uses),
			"nested job %q of %q requires %s but the calling job grants %s",
			id,
			call.Uses.Value,
			messages.Quotes(want),
			messages.Quotes(have),
		)
	}

	return findings
}

func callPosition(node *definition.String) source.Position {
	pos, _ := node.Position()
	return pos
}

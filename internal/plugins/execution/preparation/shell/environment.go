package shell

import (
	"strings"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

// EnvironmentStartup declares imported names without assigning them values.
// Only names available in every OS alternative are imported. Unresolved keys
// prevent unassigned-variable checks from deciding that a name is absent.
func EnvironmentStartup(startup string, alternatives []scriptmodel.EnvironmentNames) (string, bool) {
	uncertain := len(alternatives) == 0
	for _, environment := range alternatives {
		uncertain = uncertain || environment.UnknownNames() || environment.LookupComparison() == scriptmodel.NameUnknown
		for _, name := range environment.Names() {
			uncertain = uncertain || !name.CaseKnown
		}
	}
	if uncertain {
		return startup, false
	}
	var names []string
	for _, variable := range alternatives[0].Names() {
		if !shellVariableName(variable.Name) {
			continue
		}
		available := true
		for _, environment := range alternatives[1:] {
			available = available && environment.Lookup(variable.Name) == scriptmodel.EnvironmentDeclared
		}
		if available {
			names = append(names, variable.Name)
		}
	}
	if len(names) != 0 {
		if startup != "" {
			startup += "; "
		}
		startup += "export " + strings.Join(names, " ")
	}
	return startup, true
}

func shellVariableName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (index == 0 || character < '0' || character > '9') {
			return false
		}
	}
	return true
}

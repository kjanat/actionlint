package configuration

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
)

func MergeWarnings(groups ...[]configreport.Warning) []configreport.Warning {
	var result []configreport.Warning
	seen := map[configreport.Warning]bool{}
	for _, group := range groups {
		for _, warning := range group {
			if !seen[warning] {
				seen[warning] = true
				result = append(result, warning)
			}
		}
	}
	return result
}

func configWarnings(root any, inputs map[string]document.Location) []configreport.Warning {
	var warnings []configreport.Warning
	var check func(any, string, string, []string)
	check = func(value any, prefix, path string, allowed []string) {
		keys := make([]string, 0, len(members(value)))
		for key := range members(value) {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			child := field(value, key)
			name := prefix + key
			pointer := document.Path(path, key)
			if !slices.Contains(allowed, key) {
				loc := inputs[pointer]
				warnings = append(warnings, configreport.Warning{File: loc.File, Line: loc.KeyLine, Column: loc.KeyColumn, Message: fmt.Sprintf("unknown configuration key %q; ignored. Expected one of: %s", name, strings.Join(allowed, ", "))})
				continue
			}
			if prefix != "" {
				continue
			}
			switch key {
			case "self-hosted-runner":
				check(child, name+".", pointer, []string{"labels"})
			case "paths":
				for k, v := range members(child) {
					check(v, name+"["+k+"].", document.Path(pointer, k), []string{"ignore"})
				}
			}
		}
	}
	check(root, "", "", append(append(Keys(), legacyKeys...), "$schema"))
	slices.SortStableFunc(warnings, func(a, b configreport.Warning) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), cmp.Compare(a.Message, b.Message))
	})
	return warnings
}

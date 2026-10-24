package component

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasmparser"
)

var grantedImportPrefixes = []string{"wasi:cli/", "wasi:clocks/", "wasi:io/", "wasi:random/", "wasi:filesystem/"}

func validateSuppressionRuleName(name string) error {
	if builtin, ok := rulecatalog.Find(name); ok && builtin.Provider == "actionlint" {
		return fmt.Errorf("plugin name %q conflicts with a built-in rule", name)
	}
	if strings.TrimSpace(name) != name || strings.ContainsAny(name, ",\r\n") ||
		strings.Contains(name, " -- ") || strings.HasSuffix(name, " --") {
		return fmt.Errorf("plugin name %q cannot be used as an inline suppression rule", name)
	}
	return nil
}

type extensionRoleSet struct {
	analyzer bool
	runner   bool
	script   bool
}

func extensionRoles(exports []wacogo.ExportDesc) (extensionRoleSet, error) {
	var roles extensionRoleSet
	supported := []struct {
		name string
		role *bool
	}{{AnalyzerInterface, &roles.analyzer}, {RunnerInterface, &roles.runner}, {ScriptEngineInterface, &roles.script}}
	for _, export := range exports {
		for _, candidate := range supported {
			prefix, _, _ := strings.Cut(candidate.name, "@")
			if !strings.HasPrefix(export.Name, prefix+"@") {
				continue
			}
			if export.Name != candidate.name {
				return roles, fmt.Errorf("plugin exports %s; this host supports %s", export.Name, candidate.name)
			}
			if export.Kind != wacogo.SortInstance {
				return roles, fmt.Errorf("plugin export %s must be an interface instance", export.Name)
			}
			*candidate.role = true
		}
	}
	if !roles.analyzer && !roles.runner && !roles.script {
		return roles, fmt.Errorf("plugin does not export %s, %s or %s", AnalyzerInterface, RunnerInterface, ScriptEngineInterface)
	}
	return roles, nil
}

func checkImports(imports []wacogo.ImportDesc) error {
	hostName := wasmparser.CanonicalizeImportName(HostInterface)
	annotationsName := wasmparser.CanonicalizeImportName(SourceAnnotationsInterface)
	for _, imported := range imports {
		canonical := wasmparser.CanonicalizeImportName(imported.Name)
		if canonical == hostName || canonical == annotationsName {
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/host@") {
			return fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, HostInterface)
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/source-annotations@") {
			return fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, SourceAnnotationsInterface)
		}
		granted := false
		for _, prefix := range grantedImportPrefixes {
			if strings.HasPrefix(imported.Name, prefix) {
				granted = true
				break
			}
		}
		if !granted {
			return fmt.Errorf("plugin imports %s, which this host does not grant", imported.Name)
		}
	}
	return nil
}

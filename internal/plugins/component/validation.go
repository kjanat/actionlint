package component

import (
	"errors"
	"fmt"
	"slices"
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
	// script is the exported script-engine interface, empty without the role.
	script          string
	toolIntegration bool
	// templateEffects is the exported template-effects interface, empty
	// without the role.
	templateEffects string
}

// scriptEngineInterfaces are the script-engine versions this host accepts.
var scriptEngineInterfaces = []string{ScriptEngineInterface, ScriptEngine03Interface}

// templateEffectsPairs maps each template-effects version this host accepts
// to the script-engine version it pairs with: the one whose request and
// diagnostic its check uses.
var templateEffectsPairs = map[string]string{
	TemplateEffectsInterface:   ScriptEngineInterface,
	TemplateEffects03Interface: ScriptEngine03Interface,
}

// extensionRoles negotiates the roles a component exports. A component
// exports one script-engine version and at most one template-effects
// version, the one of the same package, whose check replaces the engine's.
func extensionRoles(exports []wacogo.ExportDesc) (extensionRoleSet, error) {
	var roles extensionRoleSet
	supported := []struct {
		name string
		role *bool
	}{{AnalyzerInterface, &roles.analyzer}, {RunnerInterface, &roles.runner}, {ToolIntegrationInterface, &roles.toolIntegration}}
	scriptPrefix, _, _ := strings.Cut(ScriptEngineInterface, "@")
	templatePrefix, _, _ := strings.Cut(TemplateEffectsInterface, "@")
	for _, export := range exports {
		if strings.HasPrefix(export.Name, scriptPrefix+"@") {
			if err := negotiateVersion(export, scriptEngineInterfaces, &roles.script, "script-engine"); err != nil {
				return roles, err
			}
			continue
		}
		if strings.HasPrefix(export.Name, templatePrefix+"@") {
			if err := negotiateVersion(export, []string{TemplateEffectsInterface, TemplateEffects03Interface}, &roles.templateEffects, "template-effects"); err != nil {
				return roles, err
			}
			continue
		}
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
	if !roles.analyzer && !roles.runner && roles.script == "" {
		return roles, fmt.Errorf("plugin does not export %s, %s or %s", AnalyzerInterface, RunnerInterface, strings.Join(scriptEngineInterfaces, " or "))
	}
	if roles.toolIntegration && !roles.analyzer {
		return roles, errors.New("tool integration requires analyzer interface")
	}
	if roles.templateEffects != "" && roles.script != templateEffectsPairs[roles.templateEffects] {
		return roles, fmt.Errorf("%s requires %s", roles.templateEffects, templateEffectsPairs[roles.templateEffects])
	}
	return roles, nil
}

// negotiateVersion records export as the component's version of a
// versioned role, stored in role. It fails for a version outside accepted,
// a non-instance export, or a second version of the role.
func negotiateVersion(export wacogo.ExportDesc, accepted []string, role *string, kind string) error {
	if !slices.Contains(accepted, export.Name) {
		return fmt.Errorf("plugin exports %s; this host supports %s", export.Name, strings.Join(accepted, " and "))
	}
	if export.Kind != wacogo.SortInstance {
		return fmt.Errorf("plugin export %s must be an interface instance", export.Name)
	}
	if *role != "" {
		return fmt.Errorf("plugin exports %s and %s; export one %s version", *role, export.Name, kind)
	}
	*role = export.Name
	return nil
}

type importRequirements struct {
	invocations       bool
	sourceAnnotations bool
	sourceFiles       bool
	preparedScripts   bool
}

// validateImports checks grants and records optional host features in one pass.
func validateImports(imports []wacogo.ImportDesc) (importRequirements, error) {
	var required importRequirements
	hostName := wasmparser.CanonicalizeImportName(HostInterface)
	annotationsName := wasmparser.CanonicalizeImportName(SourceAnnotationsInterface)
	for _, imported := range imports {
		canonical := wasmparser.CanonicalizeImportName(imported.Name)
		if canonical == wasmparser.CanonicalizeImportName(InvocationContextInterface) {
			required.invocations = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/invocation-context@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, InvocationContextInterface)
		}
		if canonical == wasmparser.CanonicalizeImportName(PreparedScriptsInterface) {
			required.preparedScripts = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/prepared-scripts@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, PreparedScriptsInterface)
		}
		if canonical == wasmparser.CanonicalizeImportName(SourceFilesInterface) {
			required.sourceFiles = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/source-files@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, SourceFilesInterface)
		}
		if canonical == annotationsName {
			required.sourceAnnotations = true
			continue
		}
		if canonical == hostName {
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/host@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, HostInterface)
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/source-annotations@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, SourceAnnotationsInterface)
		}
		granted := false
		for _, prefix := range grantedImportPrefixes {
			if strings.HasPrefix(imported.Name, prefix) {
				granted = true
				break
			}
		}
		if !granted {
			return required, fmt.Errorf("plugin imports %s, which this host does not grant", imported.Name)
		}
	}
	return required, nil
}

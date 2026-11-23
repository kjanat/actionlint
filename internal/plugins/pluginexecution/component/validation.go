package component

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"actionlint.kjanat.dev/v2/internal/model/rule"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasmparser"
)

var grantedImportPrefixes = []string{"wasi:cli/", "wasi:clocks/", "wasi:io/", "wasi:random/", "wasi:filesystem/"}

func validateSuppressionRuleName(reserved rule.Names, name string) error {
	if reserved.Contains(name) {
		return fmt.Errorf("plugin name %q conflicts with a built-in rule", name)
	}
	if name == "" || strings.ContainsAny(name, ",[]") || strings.ContainsFunc(name, unicode.IsSpace) {
		return fmt.Errorf("plugin name %q cannot be used as an inline suppression rule", name)
	}
	return nil
}

type extensionRoleSet struct {
	analyzer          bool
	runner            runnerProtocol
	script            scriptEngineProtocol
	toolIntegration   bool
	referenceResolver bool
	templateEffects   *templateEffectsProtocol
}

// extensionRoles negotiates the roles a component exports. A component
// exports one script-engine version and at most one template-effects
// version, the one of the same package, whose check replaces the engine's.
func extensionRoles(exports []wacogo.ExportDesc) (extensionRoleSet, error) {
	var roles extensionRoleSet
	supported := []struct {
		name string
		role *bool
	}{{AnalyzerInterface, &roles.analyzer}, {ToolIntegrationInterface, &roles.toolIntegration}, {ReferenceResolverInterface, &roles.referenceResolver}}
	scriptPrefix, _, _ := strings.Cut(ScriptEngineInterface, "@")
	runnerPrefix, _, _ := strings.Cut(RunnerInterface, "@")
	templatePrefix, _, _ := strings.Cut(TemplateEffectsInterface, "@")
	for _, export := range exports {
		if strings.HasPrefix(export.Name, runnerPrefix+"@") {
			if err := negotiateVersion(export, runnerProtocols, &roles.runner, "runner-provider"); err != nil {
				return roles, err
			}
			continue
		}
		if strings.HasPrefix(export.Name, scriptPrefix+"@") {
			if err := negotiateVersion(export, scriptEngineProtocols, &roles.script, "script-engine"); err != nil {
				return roles, err
			}
			continue
		}
		if strings.HasPrefix(export.Name, templatePrefix+"@") {
			if err := negotiateVersion(export, templateEffectsProtocols, &roles.templateEffects, "template-effects"); err != nil {
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
	if !roles.analyzer && roles.runner == nil && roles.script == nil && !roles.referenceResolver {
		return roles, fmt.Errorf("plugin does not export %s, %s, %s or %s", AnalyzerInterface, RunnerInterface, strings.Join(interfaceNames(scriptEngineProtocols), " or "), ReferenceResolverInterface)
	}
	if roles.toolIntegration && !roles.analyzer {
		return roles, errors.New("tool integration requires analyzer interface")
	}
	if roles.templateEffects != nil && (roles.script == nil || roles.script.templateEffects() != roles.templateEffects) {
		paired := slices.IndexFunc(scriptEngineProtocols, func(script scriptEngineProtocol) bool {
			return script.templateEffects() == roles.templateEffects
		})
		return roles, fmt.Errorf("%s requires %s", roles.templateEffects.interfaceName(), scriptEngineProtocols[paired].interfaceName())
	}
	return roles, nil
}

func negotiateVersion[V versionedRole](export wacogo.ExportDesc, accepted []V, role *V, kind string) error {
	index := slices.IndexFunc(accepted, func(version V) bool { return version.interfaceName() == export.Name })
	if index < 0 {
		return fmt.Errorf("plugin exports %s; this host supports %s", export.Name, strings.Join(interfaceNames(accepted), " and "))
	}
	if export.Kind != wacogo.SortInstance {
		return fmt.Errorf("plugin export %s must be an interface instance", export.Name)
	}
	var none V
	if *role != none {
		return fmt.Errorf("plugin exports %s and %s; export one %s version", (*role).interfaceName(), export.Name, kind)
	}
	*role = accepted[index]
	return nil
}

type importRequirements struct {
	invocations       bool
	sourceAnnotations bool
	sourceFiles       bool
	shellScriptInputs bool
	environments      bool
	repositoryObjects bool
}

func validateImports(imports []wacogo.ImportDesc) (importRequirements, error) {
	var required importRequirements
	hostName := wasmparser.CanonicalizeImportName(HostInterface)
	annotationsName := wasmparser.CanonicalizeImportName(SourceAnnotationsInterface)
	for _, imported := range imports {
		canonical := wasmparser.CanonicalizeImportName(imported.Name)
		if canonical == wasmparser.CanonicalizeImportName(RepositoryObjectsInterface) {
			required.repositoryObjects = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint-resolver/repository-objects@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, RepositoryObjectsInterface)
		}
		if canonical == wasmparser.CanonicalizeImportName(ScriptEnvironmentsInterface) {
			required.environments = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint-script/environments@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, ScriptEnvironmentsInterface)
		}
		if canonical == wasmparser.CanonicalizeImportName(InvocationContextInterface) {
			required.invocations = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/invocation-context@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, InvocationContextInterface)
		}
		if canonical == wasmparser.CanonicalizeImportName(ShellScriptInputsInterface) {
			required.shellScriptInputs = true
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint-script/shell-script-inputs@") {
			return required, fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, ShellScriptInputsInterface)
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

package component

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"actionlint.kjanat.dev/v2/internal/model/rule"
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

// negotiate derives roles from the exports and checks the imports. A component
// exports one script-engine version and at most one template-effects
// version, the one of the same package, whose check replaces the engine's.
func negotiate(interfaces Interfaces) (extensionRoleSet, importRequirements, error) {
	var roles extensionRoleSet
	supported := []struct {
		name string
		role *bool
	}{{AnalyzerInterface, &roles.analyzer}, {ToolIntegrationInterface, &roles.toolIntegration}, {ReferenceResolverInterface, &roles.referenceResolver}}
	for _, export := range interfaces.Exports {
		if sameFamily(export, RunnerInterface) {
			if err := negotiateVersion(export, runnerProtocols, &roles.runner, "runner-provider"); err != nil {
				return roles, importRequirements{}, err
			}
			continue
		}
		if sameFamily(export, ScriptEngineInterface) {
			if err := negotiateVersion(export, scriptEngineProtocols, &roles.script, "script-engine"); err != nil {
				return roles, importRequirements{}, err
			}
			continue
		}
		if sameFamily(export, TemplateEffectsInterface) {
			if err := negotiateVersion(export, templateEffectsProtocols, &roles.templateEffects, "template-effects"); err != nil {
				return roles, importRequirements{}, err
			}
			continue
		}
		for _, candidate := range supported {
			if !sameFamily(export, candidate.name) {
				continue
			}
			if export != candidate.name {
				return roles, importRequirements{}, fmt.Errorf("plugin exports %s; this host supports %s", export, candidate.name)
			}
			*candidate.role = true
		}
	}
	if !roles.analyzer && roles.runner == nil && roles.script == nil && !roles.referenceResolver {
		return roles, importRequirements{}, fmt.Errorf("plugin does not export %s, %s, %s or %s", AnalyzerInterface, RunnerInterface, strings.Join(interfaceNames(scriptEngineProtocols), " or "), ReferenceResolverInterface)
	}
	if roles.toolIntegration && !roles.analyzer {
		return roles, importRequirements{}, errors.New("tool integration requires analyzer interface")
	}
	if roles.templateEffects != nil && (roles.script == nil || roles.script.templateEffects() != roles.templateEffects) {
		paired := slices.IndexFunc(scriptEngineProtocols, func(script scriptEngineProtocol) bool {
			return script.templateEffects() == roles.templateEffects
		})
		return roles, importRequirements{}, fmt.Errorf("%s requires %s", roles.templateEffects.interfaceName(), scriptEngineProtocols[paired].interfaceName())
	}
	required, err := validateImports(interfaces.Imports)
	if err != nil {
		return roles, required, err
	}
	if required.repositoryObjects && !roles.referenceResolver {
		return roles, required, errors.New("repository objects require the reference-resolver role")
	}
	return roles, required, nil
}

func sameFamily(name, known string) bool {
	family, _, _ := strings.Cut(known, "@")
	return strings.HasPrefix(name, family+"@")
}

func negotiateVersion[V versionedRole](export string, accepted []V, role *V, kind string) error {
	index := slices.IndexFunc(accepted, func(version V) bool { return version.interfaceName() == export })
	if index < 0 {
		return fmt.Errorf("plugin exports %s; this host supports %s", export, strings.Join(interfaceNames(accepted), " and "))
	}
	var none V
	if *role != none {
		return fmt.Errorf("plugin exports %s and %s; export one %s version", (*role).interfaceName(), export, kind)
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

type hostImport struct {
	name     string
	required *bool
}

func (r *importRequirements) hostImports() []hostImport {
	return []hostImport{
		{RepositoryObjectsInterface, &r.repositoryObjects},
		{ScriptEnvironmentsInterface, &r.environments},
		{InvocationContextInterface, &r.invocations},
		{ShellScriptInputsInterface, &r.shellScriptInputs},
		{SourceFilesInterface, &r.sourceFiles},
		{SourceAnnotationsInterface, &r.sourceAnnotations},
		{HostInterface, nil},
	}
}

func validateImports(imports []string) (importRequirements, error) {
	var required importRequirements
	provided := required.hostImports()
	for _, imported := range imports {
		canonical := wasmparser.CanonicalizeImportName(imported)
		index := slices.IndexFunc(provided, func(candidate hostImport) bool {
			return canonical == wasmparser.CanonicalizeImportName(candidate.name) || sameFamily(imported, candidate.name)
		})
		if index >= 0 {
			if canonical != wasmparser.CanonicalizeImportName(provided[index].name) {
				return required, fmt.Errorf("plugin imports %s; this host provides %s", imported, provided[index].name)
			}
			if provided[index].required != nil {
				*provided[index].required = true
			}
			continue
		}
		if !slices.ContainsFunc(grantedImportPrefixes, func(prefix string) bool { return strings.HasPrefix(imported, prefix) }) {
			return required, fmt.Errorf("plugin imports %s, which this host does not grant", imported)
		}
	}
	return required, nil
}

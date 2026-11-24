package component

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"github.com/kjanat/wacogo/wasmparser"
)

type Interfaces struct {
	Imports []string `json:"imports"`
	Exports []string `json:"exports"`
}

func CheckInterfaces(interfaces Interfaces) error {
	_, _, err := negotiate(interfaces)
	return err
}

func negotiate(interfaces Interfaces) (extensionRoleSet, importRequirements, error) {
	roles, err := extensionRoles(interfaces.Exports)
	if err != nil {
		return roles, importRequirements{}, err
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

func ReadInterfaces(data []byte) (Interfaces, error) {
	interfaces, _, err := parseInterfaces(data)
	return interfaces, err
}

func parseInterfaces(data []byte) (Interfaces, *wasmparser.ComponentType, error) {
	parser := wasmparser.NewValidatingParser(bytes.NewReader(data), wasmparser.DefaultFeatures())
	interfaces := Interfaces{Imports: []string{}, Exports: []string{}}
	for {
		payload, err := parser.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Interfaces{}, nil, fmt.Errorf("validate component contract: %w", err)
		}
		switch section := payload.(type) {
		case *wasmparser.ValidatedComponentImportSectionPayload:
			for item, err := range section.Items() {
				if err != nil {
					return Interfaces{}, nil, fmt.Errorf("validate component contract: %w", err)
				}
				interfaces.Imports = append(interfaces.Imports, item.Import.Name.Name)
			}
		case *wasmparser.ValidatedComponentExportSectionPayload:
			for item, err := range section.Items() {
				if err != nil {
					return Interfaces{}, nil, fmt.Errorf("validate component contract: %w", err)
				}
				name := item.Export.Name.Name
				if item.Export.Kind == wasmparser.ExternalKindInstance {
					interfaces.Exports = append(interfaces.Exports, name)
				} else if slices.ContainsFunc(acceptedExports(), func(accepted string) bool { return sameFamily(name, accepted) }) {
					return Interfaces{}, nil, fmt.Errorf("plugin export %s must be an interface instance", name)
				}
			}
		}
	}
	component := parser.TopLevelComponentType()
	if component == nil {
		return Interfaces{}, nil, errors.New("missing component type")
	}
	return interfaces, component, nil
}

func acceptedExports() []string {
	return slices.Concat([]string{AnalyzerInterface, ToolIntegrationInterface, ReferenceResolverInterface}, interfaceNames(runnerProtocols), interfaceNames(scriptEngineProtocols), interfaceNames(templateEffectsProtocols))
}

func acceptedInterfaces() []string {
	var required importRequirements
	exports, imports := acceptedExports(), required.hostImports()
	lines := make([]string, 0, len(exports)+len(imports)+len(grantedImportPrefixes))
	for _, name := range exports {
		lines = append(lines, "export "+name)
	}
	for _, provided := range imports {
		lines = append(lines, "import "+provided.name)
	}
	for _, prefix := range grantedImportPrefixes {
		lines = append(lines, "import "+prefix)
	}
	slices.Sort(lines)
	return lines
}

func AcceptedInterfacesDigest() string {
	return sha256hex.Digest([]byte(strings.Join(acceptedInterfaces(), "\n")))
}

// validateAnalyzerInterface rejects incompatible analyzer interfaces before guest startup.
func validateAnalyzerInterface(data []byte) error {
	return validatePluginInterfaces(data, extensionRoleSet{analyzer: true})
}

func validatePluginInterfaces(data []byte, roles extensionRoleSet) error {
	_, component, err := parseInterfaces(data)
	if err != nil {
		return err
	}
	return checkRoleTypes(component, roles)
}

func checkRoleTypes(component *wasmparser.ComponentType, roles extensionRoleSet) error {
	instance, err := component.NewInstance(nil)
	if err != nil {
		return fmt.Errorf("validate component contract: %w", err)
	}
	if roles.analyzer {
		if err := expectedAnalyzerInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible analyzer contract: %w", err)
		}
	}
	if roles.referenceResolver {
		if err := expectedReferenceResolverInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible reference resolver interface: %w", err)
		}
	}
	if roles.toolIntegration {
		if err := expectedToolIntegrationInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible tool integration contract: %w", err)
		}
	}
	if roles.templateEffects != nil {
		if err := roles.templateEffects.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.templateEffects.interfaceName(), err)
		}
	}
	if roles.runner != nil {
		if err := roles.runner.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible runner contract: %w", err)
		}
	}
	if roles.script != nil {
		if err := roles.script.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.script.interfaceName(), err)
		}
	}
	return nil
}

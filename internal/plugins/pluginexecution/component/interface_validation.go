package component

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/kjanat/wacogo/wasmparser"
)

// validateAnalyzerInterface rejects incompatible analyzer interfaces before guest startup.
// The generated consumer asks for the complete canonical interface, including
// named types and nested record/enum shapes, rather than only its export name.
func validateAnalyzerInterface(data []byte) error {
	return validatePluginInterfaces(data, extensionRoleSet{analyzer: true})
}

func validatePluginInterfaces(data []byte, roles extensionRoleSet) error {
	parser := wasmparser.NewValidatingParser(bytes.NewReader(data), wasmparser.DefaultFeatures())
	for {
		_, err := parser.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("validate component contract: %w", err)
		}
	}
	component := parser.TopLevelComponentType()
	if component == nil {
		return errors.New("missing component type")
	}
	instance := component.NewInstance(nil)
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

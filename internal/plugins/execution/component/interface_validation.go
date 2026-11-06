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
	if roles.analyzer {
		if err := expectedAnalyzerInterface().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible analyzer contract: %w", err)
		}
	}
	if roles.toolIntegration {
		if err := expectedToolIntegrationInterface().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible tool integration contract: %w", err)
		}
	}
	if roles.templateEffects != "" {
		expected := expectedTemplateEffectsInterface
		if roles.templateEffects == TemplateEffects03Interface {
			expected = expectedTemplateEffects03Interface
		}
		if err := expected().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.templateEffects, err)
		}
	}
	if roles.runner {
		if err := expectedRunnerInterface().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible runner contract: %w", err)
		}
	}
	if roles.script != "" {
		expected := expectedScriptEngineInterface
		if roles.script == ScriptEngine03Interface {
			expected = expectedScriptEngine03Interface
		}
		if err := expected().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.script, err)
		}
	}
	return nil
}

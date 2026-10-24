package component

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/kjanat/wacogo/wasmparser"
)

// checkContract rejects incompatible analyzer interfaces before guest startup.
// The generated consumer asks for the complete canonical interface, including
// named types and nested record/enum shapes, rather than only its export name.
func checkContract(data []byte) error {
	return checkRoleContracts(data, extensionRoleSet{analyzer: true})
}

func checkRoleContracts(data []byte, roles extensionRoleSet) error {
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
		if err := expectedAnalyzerContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible analyzer contract: %w", err)
		}
	}
	if roles.runner {
		if err := expectedRunnerContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible runner contract: %w", err)
		}
	}
	if roles.script {
		if err := expectedScriptEngineContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible script-engine contract: %w", err)
		}
	}
	return nil
}

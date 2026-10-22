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
	return checkRoleContracts(data, true, false, false)
}

func checkRoleContracts(data []byte, analyzerRole, runnerRole, scriptRole bool) error {
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
	if analyzerRole {
		if err := expectedAnalyzerContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible analyzer contract: %w", err)
		}
	}
	if runnerRole {
		if err := expectedRunnerContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible runner contract: %w", err)
		}
	}
	if scriptRole {
		if err := expectedScriptEngineContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
			return fmt.Errorf("incompatible script-engine contract: %w", err)
		}
	}
	return nil
}

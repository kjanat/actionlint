package component

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/partite-ai/wacogo/wasmparser"
)

// checkContract rejects incompatible analyzer interfaces before guest startup.
// The generated consumer asks for the complete canonical interface, including
// named types and nested record/enum shapes, rather than only its export name.
func checkContract(data []byte) error {
	parser := wasmparser.NewValidatingParser(bytes.NewReader(data), wasmparser.DefaultFeatures())
	for {
		_, err := parser.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("validate analyzer contract: %w", err)
		}
	}
	component := parser.TopLevelComponentType()
	if component == nil {
		return errors.New("missing analyzer component type")
	}
	if err := expectedAnalyzerContract().CheckInstantiation(map[string]any{"plugin": component.NewInstance()}); err != nil {
		return fmt.Errorf("incompatible analyzer contract: %w", err)
	}
	return nil
}

package checkinputs

import (
	"errors"
	"fmt"
	"strings"
)

// ScriptInput Action is a lowercase owner/repository and optional
// case-sensitive subpath, without a ref.
type ScriptInput struct {
	Action   string `json:"action"`
	Input    string `json:"input"`
	Language string `json:"language"`
}

func ValidateScriptInputs(inputs []ScriptInput) error {
	if len(inputs) > 256 {
		return errors.New("script input selectors exceed 256 entries")
	}
	seen := make(map[[2]string]bool, len(inputs))
	for _, input := range inputs {
		if err := ValidateScriptInput(input); err != nil {
			return err
		}
		key := [2]string{input.Action, input.Input}
		if seen[key] {
			return fmt.Errorf("invalid or duplicate script input selector %+v", input)
		}
		seen[key] = true
	}
	return nil
}

func ValidateScriptInput(input ScriptInput) error {
	if len(input.Action) > 4096 || len(input.Input) > 128 || len(input.Language) > 64 {
		return errors.New("script input selector exceeds action, input or language length limits")
	}
	parts := strings.Split(input.Action, "/")
	valid := len(parts) >= 2 && selectorWord(input.Input, false) && selectorWord(input.Language, false)
	for index, part := range parts {
		valid = valid && selectorWord(part, index >= 2) && part != "." && part != ".."
	}
	if !valid {
		return fmt.Errorf("invalid script input selector %+v", input)
	}
	return nil
}

func selectorWord(word string, uppercase bool) bool {
	if word == "" {
		return false
	}
	for _, ch := range word {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.':
		case uppercase && ch >= 'A' && ch <= 'Z':
		default:
			return false
		}
	}
	return true
}

package ruff

import (
	"encoding/json"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"

	"actionlint.kjanat.dev/v2/internal/tools"
)

func prepareInput(script tools.Script) (tools.PreparedInput, error) {
	settings, err := configFor(script.Config)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	options, err := engineOptions(settings)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	masks := &templateMasks{identifiers: make(map[Position]int), parentheses: make(map[Position]Position), quoted: make(map[Position]string)}
	source, valid, err := sanitize(script.Text, ruffExpressionEnd, masks)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	contract := ""
	if strings.Contains(script.Text, "${{") {
		contract = analyzer.TemplateEffectsContract
	}
	return tools.PreparedInput{Source: source, Config: encoded, Skip: !valid, TemplateContract: contract, AcceptDiagnostic: func(d analyzer.ScriptDiagnostic) bool { return masks.accept(d) }}, nil
}

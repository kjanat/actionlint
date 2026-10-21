package ruff

import (
	"encoding/json"

	"actionlint.kjanat.dev/internal/tools"
)

func prepareInput(script tools.Script) (tools.PreparedInput, error) {
	settings := Config{}
	if script.Config != nil {
		settings = script.Config.Tools.Ruff
	}
	options, err := engineOptions(settings)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	encoded, err := json.Marshal(options)
	return tools.PreparedInput{Source: script.Text, Config: encoded}, err
}

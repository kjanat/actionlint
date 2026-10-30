package ruff

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	pythonprofile "actionlint.kjanat.dev/v2/internal/tools/profile/python"
)

func prepareInput(script tools.Script) (profile.Prepared, error) {
	options, err := engineOptions(configFor(script.Config))
	if err != nil {
		return profile.Prepared{}, err
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return profile.Prepared{}, err
	}
	prepared, err := pythonprofile.Script(script.Text)
	if err != nil {
		return profile.Prepared{}, err
	}
	prepared.Config = encoded
	return prepared, nil
}

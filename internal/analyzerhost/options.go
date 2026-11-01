package analyzerhost

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/analyzerhost/executor"
)

// ValidateOptions checks provider options independently of transport. Schemas must bundle their refs;
// loading a plugin must never grant implicit filesystem or network reads.
func ValidateOptions(schemaJSON, optionsJSON json.RawMessage) error {
	return executor.ValidateOptions(schemaJSON, optionsJSON)
}

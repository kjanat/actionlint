package application

import "actionlint.kjanat.dev/internal/engine"

// LoadActionCatalog installs the validated browser catalogue before analysis begins.
// Callers must complete bootstrap before starting concurrent sessions.
func LoadActionCatalog(data []byte) error { return engine.LoadPopularActions(data) }

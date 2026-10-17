package application

import "actionlint.kjanat.dev/internal/actioncatalog"

// LoadActionCatalog installs the validated browser catalogue before analysis begins.
// Callers must complete bootstrap before starting concurrent sessions.
func LoadActionCatalog(data []byte) error { return actioncatalog.Load(data) }

package component

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/guestlimits"
)

const (
	// DefaultMemoryPages bounds live linear memory and reserved core tables.
	DefaultMemoryPages uint32 = 1024
	maxLiftBytes              = 8 * 1024 * 1024
	maxLiftValues             = 262144
)

// Options configures a loaded plugin. Zero values select the defaults.
type Options struct {
	ReservedRules rule.Names
	Artifact      artifacts.Options
	guestlimits.Limits
	// Log receives host log lines and the plugin's stdout and stderr lines.
	Log func(string)
}

func (o Options) limits() (guestlimits.Limits, error) {
	limits, err := o.Resolve(DefaultMemoryPages)
	if err != nil {
		return guestlimits.Limits{}, fmt.Errorf("plugin %w", err)
	}
	return limits, nil
}

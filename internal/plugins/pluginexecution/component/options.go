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
	// ReservedRules identifies names and aliases the caller reserves for native checks.
	ReservedRules rule.Names
	// Artifact configures URI resolution and verified download caching.
	Artifact artifacts.Options
	// Limits requests the guest's resource bounds; zero values select
	// guestlimits.DefaultTimeout and DefaultMemoryPages.
	guestlimits.Limits
	// Log receives host log lines and the plugin's stdout and stderr lines.
	Log func(string)
}

// limits validates resource policy before artifact resolution or runtime creation.
func (o Options) limits() (guestlimits.Limits, error) {
	limits, err := o.Resolve(DefaultMemoryPages)
	if err != nil {
		return guestlimits.Limits{}, fmt.Errorf("plugin %w", err)
	}
	return limits, nil
}

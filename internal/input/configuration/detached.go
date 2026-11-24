package configuration

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"
)

// Detached skips revalidation.
func (cfg *Config) Detached() (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	copied, err := configdecode.DetachSnapshot(cfg)
	if err != nil {
		return nil, fmt.Errorf("copy configuration: %w", err)
	}
	return copied, nil
}

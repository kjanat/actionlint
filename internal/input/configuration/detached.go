package configuration

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

// Detached returns an independent typed configuration without revalidating it.
func (cfg *Config) Detached() (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	copied, err := binding.DetachSnapshot(cfg)
	if err != nil {
		return nil, fmt.Errorf("copy configuration: %w", err)
	}
	return copied, nil
}

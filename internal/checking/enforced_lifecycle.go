package checking

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configsource"
)

type RequiredPolicyError struct{ Err error }

func (e *RequiredPolicyError) Error() string {
	return fmt.Sprintf("load required lifecycle policy: %v", e.Err)
}
func (e *RequiredPolicyError) Unwrap() error { return e.Err }

func prepareEnforcedLifecycle(ctx context.Context, options Options) (Options, error) {
	if len(options.EnforcePolicies) == 0 {
		return options, nil
	}
	paths := make([]string, 0, len(options.EnforcePolicies))
	for _, path := range options.EnforcePolicies {
		if strings.TrimSpace(path) != "" && !configsource.IsRemoteAddress(path) && (!strings.Contains(path, ":") || filepath.VolumeName(path) != "") && !filepath.IsAbs(path) && options.WorkingDir != "" {
			path = filepath.Join(options.WorkingDir, path)
		}
		paths = append(paths, path)
	}
	reader := configsource.New(ctx, options.ReadFile, options.ConfigConnections)
	loaded, err := configuration.ReadEnforcedLifecyclePolicies(paths, reader.ReadConfig)
	if err != nil {
		return options, &RequiredPolicyError{Err: err}
	}
	options.EnforcedLifecycle = slices.Concat(options.EnforcedLifecycle, loaded.Policies())
	options.enforcedPolicyInputs = slices.Concat(options.enforcedPolicyInputs, loaded.Files())
	options.EnforcePolicies = nil
	return options, nil
}

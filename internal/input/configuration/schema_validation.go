package config

import (
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

// schemaBindingError preserves detached schema paths for source attribution.
func schemaBindingError(err error) error {
	if err == nil {
		return nil
	}
	validation, ok := errors.AsType[*configschema.ValidationError](err)
	if !ok {
		return err
	}
	var failures []error
	for _, failure := range validation.Failures {
		at := failure.Path
		if at == "" {
			at = "/"
		}
		message := fmt.Sprintf("at %s: %s", at, failure.Message)
		if len(failure.UnknownKeys) == 0 {
			failures = append(failures, &binding.Error{Path: failure.Path, Cause: errors.New(message)})
			continue
		}
		for _, key := range failure.UnknownKeys {
			pointer := failure.Path + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
			failures = append(failures, &binding.Error{Path: pointer, Cause: fmt.Errorf("%s: %w", message, binding.ErrUnknownKey)})
		}
	}
	return errors.Join(failures...)
}

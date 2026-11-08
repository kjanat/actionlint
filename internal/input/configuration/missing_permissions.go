package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

type PermissionLocation string

const (
	WorkflowPermissions PermissionLocation = "workflow"
	EveryJobPermissions PermissionLocation = "every-job"
)

func PermissionLocations() []PermissionLocation {
	return []PermissionLocation{WorkflowPermissions, EveryJobPermissions}
}

// PermissionLocationList accepts one location or a nonempty array of locations.
type PermissionLocationList []PermissionLocation

func (locations *PermissionLocationList) DecodeConfig(value any) error {
	var values []string
	_, scalarInput := value.(string)
	if scalar, ok := value.(string); ok {
		values = []string{scalar}
	} else if err := binding.Decode(value, &values); err != nil {
		return fmt.Errorf("require-at must be a string or an array of locations: %w", err)
	}
	if len(values) == 0 {
		return errors.New("require-at must contain at least one location; set missing-permissions: off to disable the rule")
	}
	var result PermissionLocationList
	for i, value := range values {
		location := PermissionLocation(value)
		if !slices.Contains(PermissionLocations(), location) {
			choices := make([]string, 0, len(PermissionLocations()))
			for _, choice := range PermissionLocations() {
				choices = append(choices, string(choice))
			}
			err := fmt.Errorf("unknown require-at location %q; use %s", value, strings.Join(choices, " or "))
			if scalarInput {
				return err
			}
			return binding.At(strconv.Itoa(i), err)
		}
		if !slices.Contains(result, location) {
			result = append(result, location)
		}
	}
	*locations = result
	return nil
}

type MissingPermissionsOptions struct {
	RequireAt PermissionLocationList `yaml:"require-at,omitempty" jsonschema:"description=Allowed locations for permissions. Use a location string or a nonempty array. Set the rule to off to disable it."`
}

func (options *MissingPermissionsOptions) DecodeConfig(value any) error {
	if _, ok := value.(map[string]any); !ok {
		return errors.New("missing-permissions options must be a mapping")
	}
	type plain MissingPermissionsOptions
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	*options = MissingPermissionsOptions(next)
	return nil
}

func (options MissingPermissionsOptions) Locations() []PermissionLocation {
	if options.RequireAt == nil {
		return PermissionLocations()
	}
	return slices.Clone(options.RequireAt)
}

func (cfg *Config) PermissionDeclarationLocations() ([]PermissionLocation, error) {
	options := MissingPermissionsOptions{}
	if cfg != nil {
		setting, _ := cfg.Lint.Rules.Security.Rule("missing-permissions")
		if _, err := setting.LoadOptions(&options); err != nil {
			return nil, err
		}
	}
	return options.Locations(), nil
}

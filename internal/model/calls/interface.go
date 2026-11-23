package calls

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type Input struct {
	Name string `json:"name"`
	// Required is true when the caller must supply the input: the declaration
	// sets "required: true" and supplies no default value.
	Required bool `json:"required"`
	// Reusable workflows cannot deprecate inputs.
	Deprecated         bool   `json:"deprecated"`
	DeprecationMessage string `json:"deprecation-message"`
	// Type is a reusable workflow's declared input type. Every action input
	// has WorkflowCallEventInputTypeInvalid, which accepts every value.
	Type definition.WorkflowCallEventInputType `json:"-"`
}

// Inputs matches input names case insensitively for actions and reusable workflows.
type Inputs map[string]Input

func (inputs Inputs) Names() []string {
	return declaredNames(inputs, func(Input) bool { return true }, func(input Input) string { return input.Name })
}

func (inputs Inputs) RequiredNames() []string {
	return declaredNames(inputs, func(input Input) bool { return input.Required }, func(input Input) string { return input.Name })
}

func (inputs Inputs) MissingRequired(supplied func(id string) bool) []Input {
	return missingRequired(inputs, func(input Input) bool { return input.Required }, supplied)
}

type Secret struct {
	Name string
	// When this value is true, workflow calls must set this secret unless they inherit secrets.
	Required bool
}

// Secrets matches workflow call secret names case insensitively.
type Secrets map[string]Secret

func (secrets Secrets) Names() []string {
	return declaredNames(secrets, func(Secret) bool { return true }, func(secret Secret) string { return secret.Name })
}

func (secrets Secrets) MissingRequired(supplied func(id string) bool) []Secret {
	return missingRequired(secrets, func(secret Secret) bool { return secret.Required }, supplied)
}

type Output struct {
	Name string `json:"name"`
}

// Outputs matches output names case insensitively for actions and reusable workflows.
type Outputs map[string]Output

func (outputs Outputs) Names() []string {
	return declaredNames(outputs, func(Output) bool { return true }, func(output Output) string { return output.Name })
}

func declaredNames[T any](declarations map[string]T, keep func(T) bool, name func(T) string) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		if keep(declaration) {
			names = append(names, name(declaration))
		}
	}
	slices.Sort(names)
	return names
}

func missingRequired[T any](declarations map[string]T, required func(T) bool, supplied func(id string) bool) []T {
	var missing []T
	for _, id := range slices.Sorted(maps.Keys(declarations)) {
		if declaration := declarations[id]; required(declaration) && !supplied(id) {
			missing = append(missing, declaration)
		}
	}
	return missing
}

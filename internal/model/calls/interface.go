package calls

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Input is an input declared by a callable: an "inputs" entry of an action
// manifest or of a reusable workflow's on.workflow_call. A field that only
// one kind of callable declares keeps its zero value for the other.
type Input struct {
	// Name is the input name as spelled in the declaration.
	Name string `json:"name"`
	// Required is true when the caller must supply the input: the declaration
	// sets "required: true" and supplies no default value.
	Required bool `json:"required"`
	// Deprecated is true when an action marks the input deprecated.
	// Reusable workflows cannot deprecate inputs.
	Deprecated bool `json:"deprecated"`
	// DeprecationMessage is an action's deprecation message for the input.
	DeprecationMessage string `json:"deprecation-message"`
	// Type is a reusable workflow's declared input type. GitHub accepts only
	// boolean, number and string; any other or omitted type, and every action
	// input, is WorkflowCallEventInputTypeInvalid and accepts every value.
	Type definition.WorkflowCallEventInputType `json:"-"`
}

// Inputs maps lowercase input IDs to a callable's input declarations.
// Actions and reusable workflows both match input names case insensitively.
type Inputs map[string]Input

// Names returns the declared spelling of every input, sorted.
func (inputs Inputs) Names() []string {
	return declaredNames(inputs, func(Input) bool { return true }, func(input Input) string { return input.Name })
}

// RequiredNames returns the declared spelling of every required input, sorted.
func (inputs Inputs) RequiredNames() []string {
	return declaredNames(inputs, func(input Input) bool { return input.Required }, func(input Input) string { return input.Name })
}

// MissingRequired returns the required inputs whose IDs supplied rejects, in ID order.
func (inputs Inputs) MissingRequired(supplied func(id string) bool) []Input {
	return missingRequired(inputs, func(input Input) bool { return input.Required }, supplied)
}

// Secret is a secret declared by a reusable workflow. Actions declare no secrets.
type Secret struct {
	// Name is the secret name as spelled in the declaration.
	Name string
	// Required indicates whether the reusable workflow requires the secret. When this value
	// is true, workflow calls must set this secret unless they inherit secrets.
	Required bool
}

// Secrets maps lowercase secret IDs to reusable workflow secret declarations.
// Workflow call secret names are case insensitive.
type Secrets map[string]Secret

// Names returns the declared spelling of every secret, sorted.
func (secrets Secrets) Names() []string {
	return declaredNames(secrets, func(Secret) bool { return true }, func(secret Secret) string { return secret.Name })
}

// MissingRequired returns the required secrets whose IDs supplied rejects, in ID order.
func (secrets Secrets) MissingRequired(supplied func(id string) bool) []Secret {
	return missingRequired(secrets, func(secret Secret) bool { return secret.Required }, supplied)
}

// Output is an output declared by a callable: an "outputs" entry of an action
// manifest or of a reusable workflow's on.workflow_call.
type Output struct {
	// Name is the output name as spelled in the declaration.
	Name string `json:"name"`
}

// Outputs maps lowercase output IDs to a callable's output declarations.
// Actions and reusable workflows both match output names case insensitively.
type Outputs map[string]Output

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

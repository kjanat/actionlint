package shell

import "slices"

// Settings override the defaults the profile derives from the workflow.
// A nil Config inherits the project selection; absent project config
// disables rc discovery.
type Settings struct {
	Config          RCSelection
	Shell           string
	Exclude         []string
	ExternalSources *bool
}

// RCSelection selects an rc file, discovery, or disabled rc loading.
type RCSelection interface{ rcSelection() }

// RCFile selects an explicit rc file.
// Relative application paths use the process working directory. Path
// interpolations use the analyzed project's configuration and action context.
type RCFile string

func (RCFile) rcSelection() {}

// RCMode selects discovery or disables rc loading.
type RCMode bool

func (RCMode) rcSelection() {}

const (
	// RCDisabled disables rc loading, including project-selected paths.
	RCDisabled RCMode = false
	// RCDiscover discovers configuration from the analysis working directory.
	RCDiscover RCMode = true
)

// Excluded lists the checks masked templates and the CI runtime turn into
// false positives, followed by the codes of extra not already listed:
//
//   - SC1091: File not found. Scripts are for CI environment. Not suitable for
//     checking this in current local environment.
//   - SC2194: The word is constant. This sometimes happens at constants by
//     replacing ${{ }} with underscores. For example,
//     `if ${{ matrix.foo }}; then ...` -> `if _________________; then ...`.
//   - SC2050: The expression is constant. This sometimes happens at `if`
//     condition by replacing ${{ }} with underscores (#45). For example,
//     `if [ "${{ matrix.foo }}" = "x" ]` -> `if [ "_________________" = "x" ]`.
//   - SC2153: Same as SC2154.
//   - SC2154: The var is referenced but not assigned. Script at `run:` can refer
//     variables defined in `env:` section so this rule can cause false
//     positives (#53).
//   - SC2157: Argument to -z is always false due to literal strings. When the
//     argument of -z is replaced from ${{ }}, this can happen. For example,
//     `if [ -z ${{ env.FOO }} ]` -> `if [ -z ______________ ]` (#113).
//   - SC2043: Loop can be detected as only running once when the target of
//     iteration is a placeholder (#355), e.g. `for foo in ${{ inputs.foo }}; do`.
func Excluded(extra []string) []string {
	excluded := []string{"SC1091", "SC2194", "SC2050", "SC2153", "SC2154", "SC2157", "SC2043"}
	for _, code := range extra {
		if !slices.Contains(excluded, code) {
			excluded = append(excluded, code)
		}
	}
	return excluded
}

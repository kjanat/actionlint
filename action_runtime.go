package actionlint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ActionRuntime describes a JavaScript runtime accepted by the runner's action metadata parser.
// Removed refers to the bundled executable, not whether runs.using is valid syntax.
type ActionRuntime struct {
	Removed        bool
	Deprecated     bool
	DeprecationURL string
	RemovalDate    string
}

func validActionRuntimes() string {
	names := append([]string{"composite", "docker"}, slices.Sorted(maps.Keys(ActionRuntimes))...)
	for i, name := range names {
		names[i] = strconv.Quote(name)
	}
	return strings.Join(names, ", ")
}

func actionRuntimeProblem(using string) string {
	r, ok := ActionRuntimes[strings.ToLower(using)]
	if !ok || !r.Removed && !r.Deprecated {
		return ""
	}
	if r.Removed {
		return fmt.Sprintf("runtime %q is no longer bundled with current GitHub Actions runners", using)
	}
	message := fmt.Sprintf("runtime %q is deprecated in GitHub Actions", using)
	if r.RemovalDate != "" {
		message += "; removal is scheduled for " + r.RemovalDate
	}
	return message + ". see " + r.DeprecationURL
}

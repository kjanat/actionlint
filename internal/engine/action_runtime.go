package engine

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ActionRuntime describes a JavaScript runtime accepted by the runner's action metadata parser.
// Removed means the runner no longer bundles its executable; the metadata value remains accepted.
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
	// Hosted execution policy does not change accepted metadata or bundled runtimes.
	if strings.EqualFold(using, "node20") {
		return fmt.Sprintf("runtime %q is deprecated; GitHub.com now runs JavaScript actions with Node 24 and no longer supports the Node 20 opt-out. see https://github.blog/changelog/2026-09-23-node-20-is-no-longer-available-in-github-actions/", using)
	}
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

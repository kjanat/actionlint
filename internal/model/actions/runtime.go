package action

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Runtime describes a JavaScript runtime accepted by the runner's action metadata parser.
type Runtime struct {
	Removed        bool
	Deprecated     bool
	DeprecationURL string
	RemovalDate    string
}

// ValidRuntimes lists accepted manifest runtime names for diagnostic messages.
func ValidRuntimes() string {
	names := append([]string{"composite", "docker"}, slices.Sorted(maps.Keys(Runtimes))...)
	for i, name := range names {
		names[i] = strconv.Quote(name)
	}
	return strings.Join(names, ", ")
}

// RuntimeProblem describes a runtime lifecycle problem, or returns an empty string.
func RuntimeProblem(using string) string {
	// Hosted execution policy does not change accepted metadata or bundled runtimes.
	if strings.EqualFold(using, "node20") {
		return fmt.Sprintf("runtime %q is deprecated; GitHub.com now runs JavaScript actions with Node 24 and no longer supports the Node 20 opt-out. see https://github.blog/changelog/2026-09-23-node-20-is-no-longer-available-in-github-actions/", using)
	}
	r, ok := Runtimes[strings.ToLower(using)]
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

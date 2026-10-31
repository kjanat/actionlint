// Package repository describes repository facts independently of configuration and fetching.
package repository

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/knowledge"
)

// Permissions is the repository's default workflow token setting.
// Its zero value is unavailable, rather than an assumed read grant.
type Permissions struct {
	write, known bool
	missing      knowledge.Unavailability
}

// ParsePermissions accepts the repository setting's read and write values.
func ParsePermissions(value string) (Permissions, error) {
	switch value {
	case "read":
		return Permissions{known: true}, nil
	case "write":
		return Permissions{write: true, known: true}, nil
	default:
		return Permissions{}, fmt.Errorf("default-workflow-permissions must be read or write, got %q", value)
	}
}

func (p Permissions) String() string {
	if !p.known {
		return ""
	}
	if p.write {
		return "write"
	}
	return "read"
}

func (p Permissions) Available() bool            { return p.known }
func (p Permissions) Value() (Permissions, bool) { return p, p.known }
func (p Permissions) Reason() knowledge.Reason {
	if p.known {
		return ""
	}
	return p.missing.Reason()
}

func UnavailablePermissions(reason knowledge.Unavailability) Permissions {
	return Permissions{missing: reason}
}

// Names owns a case-insensitive set of names in their original spelling.
type Names struct {
	values  []string
	missing knowledge.Unavailability
}

// NewNames rejects blank and duplicate names and owns a copy of the input.
func NewNames(values ...string) (Names, error) {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return Names{}, errors.New("name must not be blank")
		}
		key := strings.ToLower(value)
		if seen[key] {
			return Names{}, fmt.Errorf("duplicate name %q", value)
		}
		seen[key] = true
	}
	return Names{values: append([]string{}, values...)}, nil
}

// Values returns an owned list, including a non-nil list for an empty set.
func (n Names) Values() []string {
	if n.values == nil {
		return nil
	}
	return append([]string{}, n.values...)
}
func (n Names) Available() bool      { return n.values != nil }
func (n Names) Value() (Names, bool) { return n, n.Available() }
func (n Names) Reason() knowledge.Reason {
	if n.Available() {
		return ""
	}
	return n.missing.Reason()
}
func UnavailableNames(reason knowledge.Unavailability) Names { return Names{missing: reason} }

// Variable records a known name and an optional value. Unknown values do not
// make the name unavailable. Consumers must not include values in diagnostics.
type Variable struct {
	Name  string
	Value knowledge.String
}

// Variables owns variable names and their optional values.
type Variables struct {
	entries []Variable
	missing knowledge.Unavailability
}

func NewVariables(entries ...Variable) (Variables, error) {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	if _, err := NewNames(names...); err != nil {
		return Variables{}, err
	}
	return Variables{entries: append([]Variable{}, entries...)}, nil
}

func (v Variables) Available() bool          { return v.entries != nil }
func (v Variables) Value() (Variables, bool) { return v, v.Available() }
func (v Variables) Reason() knowledge.Reason {
	if v.Available() {
		return ""
	}
	return v.missing.Reason()
}

func UnavailableVariables(reason knowledge.Unavailability) Variables {
	return Variables{missing: reason}
}

func (v Variables) Entries() []Variable { return slices.Clone(v.entries) }
func (v Variables) Names() []string {
	if v.entries == nil {
		return nil
	}
	names := make([]string, len(v.entries))
	for i, entry := range v.entries {
		names[i] = entry.Name
	}
	return names
}

// Environment contains the names visible within one deployment environment.
type Environment struct {
	Name      string
	Variables Variables
	Secrets   Names
}

type Environments struct {
	entries []Environment
	missing knowledge.Unavailability
}

func NewEnvironments(entries ...Environment) (Environments, error) {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	if _, err := NewNames(names...); err != nil {
		return Environments{}, err
	}
	return Environments{entries: append([]Environment{}, entries...)}, nil
}
func (e Environments) Entries() []Environment      { return slices.Clone(e.entries) }
func (e Environments) Available() bool             { return e.entries != nil }
func (e Environments) Value() (Environments, bool) { return e, e.Available() }
func (e Environments) Reason() knowledge.Reason {
	if e.Available() {
		return ""
	}
	return e.missing.Reason()
}

func UnavailableEnvironments(reason knowledge.Unavailability) Environments {
	return Environments{missing: reason}
}

// Snapshot is host knowledge for one repository. Each field is independently
// available, so a denied lookup cannot erase facts obtained by other lookups.
type Snapshot struct {
	DefaultWorkflowPermissions Permissions
	RunnerLabels               Names
	Variables                  Variables
	Secrets                    Names
	Environments               Environments
}

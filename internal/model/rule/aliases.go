package rule

import (
	"fmt"
	"strings"
)

// Alias declares another accepted spelling of the rule that contains it.
// Deprecated aliases work, but editors should recommend the rule's Name instead.
type Alias struct {
	Name       string `json:"name"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

// Names indexes current names and aliases from a set of rule declarations.
// The zero value accepts no declared names and passes unknown names through.
type Names struct {
	targets map[string]string
	// keep, when set, limits the index to the rules whose current name it accepts.
	keep func(target string) bool
}

// Contains reports whether a current name or alias was declared in this index.
func (n Names) Contains(name string) bool {
	_, found := n.target(name)
	return found
}

// Restrict returns a view of n that holds only the rules whose current name
// keep accepts, with their aliases. The view shares n's index.
func (n Names) Restrict(keep func(target string) bool) Names {
	if n.keep == nil {
		return Names{targets: n.targets, keep: keep}
	}
	outer := n.keep
	return Names{targets: n.targets, keep: func(target string) bool { return outer(target) && keep(target) }}
}

// target returns the current name declared for name in this index.
func (n Names) target(name string) (string, bool) {
	target, found := n.targets[name]
	if !found || n.keep != nil && !n.keep(target) {
		return "", false
	}
	return target, true
}

// NewNames rejects duplicate names and aliases, including aliases that shadow
// another rule. Every alias points directly to its declaring rule.
func NewNames(descriptors []Descriptor) (Names, error) {
	result := Names{targets: make(map[string]string)}
	add := func(name, target string) error {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, " \t\r\n") {
			return fmt.Errorf("invalid rule name %q", name)
		}
		if previous, exists := result.targets[name]; exists {
			return fmt.Errorf("rule name %q is declared by both %q and %q", name, previous, target)
		}
		result.targets[name] = target
		return nil
	}
	for _, descriptor := range descriptors {
		if err := add(descriptor.Name, descriptor.Name); err != nil {
			return Names{}, err
		}
		for _, alias := range descriptor.Aliases {
			if err := add(alias.Name, descriptor.Name); err != nil {
				return Names{}, err
			}
		}
	}
	return result, nil
}

// ResolveAlias returns the current name for a declared spelling.
// Unknown names pass through unchanged.
func (n Names) ResolveAlias(name string) string {
	if target, exists := n.target(name); exists {
		return target
	}
	return name
}

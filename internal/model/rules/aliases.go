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
}

// Contains reports whether a current name or alias was declared in this index.
func (n Names) Contains(name string) bool {
	_, found := n.targets[name]
	return found
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
	if target, exists := n.targets[name]; exists {
		return target
	}
	return name
}

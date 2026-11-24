package rule

import (
	"fmt"
	"strings"
)

type Alias struct {
	Name       string `json:"name"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

// Names accepts no declared names when it is the zero value.
type Names struct {
	targets map[string]string
	keep    func(target string) bool
}

func (n Names) Contains(name string) bool {
	_, found := n.target(name)
	return found
}

// Restrict returns a view that shares n's index.
func (n Names) Restrict(keep func(target string) bool) Names {
	if n.keep == nil {
		return Names{targets: n.targets, keep: keep}
	}
	outer := n.keep
	return Names{targets: n.targets, keep: func(target string) bool { return outer(target) && keep(target) }}
}

func (n Names) target(name string) (string, bool) {
	target, found := n.targets[name]
	if !found || n.keep != nil && !n.keep(target) {
		return "", false
	}
	return target, true
}

// NewNames rejects duplicate names and aliases, including aliases that shadow another rule.
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

// ResolveAlias passes unknown names through unchanged.
func (n Names) ResolveAlias(name string) string {
	if target, exists := n.target(name); exists {
		return target
	}
	return name
}

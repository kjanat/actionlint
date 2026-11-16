// Package names models workflow-name inventories and their filter grammar.
package names

import (
	"slices"
	"strings"
)

// Inventory is a detached name set. An incomplete set cannot prove that a
// producer is absent, even when none of its known names match a filter.
type Inventory struct {
	values   map[string]bool
	literals []NamedWorkflow
	complete bool
}

// NamedWorkflow is a workflow file whose top-level name is plain text.
type NamedWorkflow struct {
	// Path is the workflow file's path as the inventory read it.
	Path string
	// Name is the name exactly as written.
	Name string
}

// WithLiteralNames returns a copy of the inventory that also records the
// workflows whose names are plain text. Later changes to the supplied slice
// do not change the inventory.
func (n Inventory) WithLiteralNames(workflows []NamedWorkflow) Inventory {
	n.literals = slices.Clone(workflows)
	return n
}

// ExcludingLiteral returns a copy of the inventory without the plain-text
// names of the workflows whose path matches. Filter names are unchanged.
func (n Inventory) ExcludingLiteral(matches func(path string) bool) Inventory {
	n.literals = slices.DeleteFunc(slices.Clone(n.literals), func(workflow NamedWorkflow) bool {
		return matches(workflow.Path)
	})
	return n
}

// SharingName returns the recorded workflows whose plain-text name equals
// name, ignoring case. The result belongs to the caller.
func (n Inventory) SharingName(name string) []NamedWorkflow {
	var matches []NamedWorkflow
	for _, workflow := range n.literals {
		if strings.EqualFold(workflow.Name, name) {
			matches = append(matches, workflow)
		}
	}
	return matches
}

// NewInventory copies values; later changes to the supplied slice do not change
// the inventory. Set complete only when every relevant source name was read.
func NewInventory(values []string, complete bool) Inventory {
	names := make(map[string]bool, len(values))
	for _, name := range values {
		names[name] = true
	}
	return Inventory{values: names, complete: complete}
}

func (n Inventory) Complete() bool { return n.complete }

func (n Inventory) Len() int { return len(n.values) }

// Contains tests an exact, case-sensitive name rather than a filter pattern.
func (n Inventory) Contains(name string) bool { return n.values[name] }

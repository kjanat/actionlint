// Package names models workflow-name inventories and their filter grammar.
package names

import (
	"slices"
	"strings"
)

// Inventory cannot prove a producer absent when the set is incomplete.
type Inventory struct {
	values   map[string]bool
	literals []NamedWorkflow
	complete bool
}

type NamedWorkflow struct {
	Path string
	Name string
}

func (n Inventory) WithLiteralNames(workflows []NamedWorkflow) Inventory {
	n.literals = slices.Clone(workflows)
	return n
}

func (n Inventory) ExcludingLiteral(matches func(path string) bool) Inventory {
	n.literals = slices.DeleteFunc(slices.Clone(n.literals), func(workflow NamedWorkflow) bool {
		return matches(workflow.Path)
	})
	return n
}

// SharingName compares names case-insensitively.
func (n Inventory) SharingName(name string) []NamedWorkflow {
	var matches []NamedWorkflow
	for _, workflow := range n.literals {
		if strings.EqualFold(workflow.Name, name) {
			matches = append(matches, workflow)
		}
	}
	return matches
}

// NewInventory takes complete as true only when every relevant source name was read.
func NewInventory(values []string, complete bool) Inventory {
	names := make(map[string]bool, len(values))
	for _, name := range values {
		names[name] = true
	}
	return Inventory{values: names, complete: complete}
}

func (n Inventory) Complete() bool { return n.complete }

func (n Inventory) Len() int { return len(n.values) }

// Contains compares names case-sensitively.
func (n Inventory) Contains(name string) bool { return n.values[name] }

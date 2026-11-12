// Package names models workflow-name inventories and their filter grammar.
package names

// Inventory is a detached name set. An incomplete set cannot prove that a
// producer is absent, even when none of its known names match a filter.
type Inventory struct {
	values   map[string]bool
	complete bool
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

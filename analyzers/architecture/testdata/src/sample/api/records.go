package api

import "slices"

// Entry pairs an exported path with a read-only handle. Cloning Path detaches
// a copy: the shared handle exposes nothing writable.
type Entry struct {
	Path   []string
	Handle Handle
}

// WritableEntry shares its handle's owner even after Path is cloned.
type WritableEntry struct {
	Path   []string
	Handle WritableHandle
}

type Catalog struct {
	entries  []Entry
	writable []WritableEntry
}

func (c *Catalog) At(i int) Entry {
	entry := c.entries[i]
	entry.Path = slices.Clone(entry.Path)
	return entry
}

func (c *Catalog) WritableAt(i int) WritableEntry { // want "ARC003: WritableAt result 1"
	entry := c.writable[i]
	entry.Path = slices.Clone(entry.Path)
	return entry
}

// Shared keeps Path shared, so the copy still exposes it.
func (c *Catalog) Shared(i int) Entry { // want "ARC003: Shared result 1"
	entry := c.entries[i]
	entry.Handle = Handle{}
	return entry
}

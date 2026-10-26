// Package architecture validates dependency contracts independently of repository layout.
package architecture

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
)

// Layer groups package subtrees with the same dependency permissions.
// Uses names direct dependencies; their dependencies are allowed too.
type Layer struct {
	Name       string   `json:"name"`
	Roots      []string `json:"roots"`
	Uses       []string `json:"uses"`
	Portable   bool     `json:"portable,omitempty"`
	EntryPoint bool     `json:"entrypoint,omitempty"`
	TestOnly   bool     `json:"testonly,omitempty"`
}

type definition struct {
	Module string  `json:"module"`
	Layers []Layer `json:"layers"`
}

// Contract is immutable after validation. The longest matching root owns a package.
// A root of "." matches only the module package, never its children.
type Contract struct {
	fingerprint string
	module      string
	layers      map[string]Layer
	roots       []string
	owners      map[string]string
	allowed     map[string]map[string]bool
}

func Load(filename string) (*Contract, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("architecture contract: %w", err)
	}
	defer f.Close()
	c, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("architecture contract %s: %w", filename, err)
	}
	return c, nil
}

func Parse(r io.Reader) (*Contract, error) {
	var d definition
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON object, got trailing input")
	}
	if !validPath(d.Module) || d.Module == "." {
		return nil, fmt.Errorf("invalid module %q", d.Module)
	}
	if len(d.Layers) == 0 {
		return nil, fmt.Errorf("no architecture layers declared")
	}
	c := &Contract{module: d.Module, layers: map[string]Layer{}, owners: map[string]string{}, allowed: map[string]map[string]bool{}}
	for _, l := range d.Layers {
		if l.Name == "" || strings.TrimSpace(l.Name) != l.Name {
			return nil, fmt.Errorf("invalid layer name %q", l.Name)
		}
		if _, exists := c.layers[l.Name]; exists {
			return nil, fmt.Errorf("duplicate layer %q", l.Name)
		}
		if len(l.Roots) == 0 {
			return nil, fmt.Errorf("layer %q has no package roots", l.Name)
		}
		c.layers[l.Name] = l
		for _, root := range l.Roots {
			if !validPath(root) {
				return nil, fmt.Errorf("layer %q: invalid package root %q", l.Name, root)
			}
			if owner, exists := c.owners[root]; exists {
				return nil, fmt.Errorf("package root %q belongs to both %q and %q", root, owner, l.Name)
			}
			c.owners[root] = l.Name
			c.roots = append(c.roots, root)
		}
	}
	// Resolve the graph once, rejecting unknown layers and dependency cycles.
	visiting := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("dependency cycle at layer %q", name)
		}
		if c.allowed[name] != nil {
			return nil
		}
		l, exists := c.layers[name]
		if !exists {
			return fmt.Errorf("unknown layer %q", name)
		}
		visiting[name] = true
		permitted := map[string]bool{name: true}
		seen := map[string]bool{}
		for _, dep := range l.Uses {
			if seen[dep] {
				return fmt.Errorf("layer %q repeats dependency %q", name, dep)
			}
			seen[dep] = true
			if target, exists := c.layers[dep]; exists && target.TestOnly && !l.TestOnly {
				return fmt.Errorf("production layer %q cannot use test-only layer %q", name, dep)
			}
			if err := visit(dep); err != nil {
				return fmt.Errorf("layer %q: %w", name, err)
			}
			for target := range c.allowed[dep] {
				permitted[target] = true
			}
		}
		visiting[name] = false
		c.allowed[name] = permitted
		return nil
	}
	for _, l := range d.Layers {
		if err := visit(l.Name); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(c.roots, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	canonical, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c.fingerprint = fmt.Sprintf("%x", sha256.Sum256(canonical))
	return c, nil
}

func validPath(p string) bool {
	return p != "" && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "\\*?[] \t\r\n")
}

// Fingerprint distinguishes analyzer caches when the contract changes.
func (c *Contract) Fingerprint() string { return c.fingerprint }

func (c *Contract) Module() string { return c.module }
func (c *Contract) Contains(p string) bool {
	return p == c.module || strings.HasPrefix(p, c.module+"/")
}

func (c *Contract) Layer(p string) (Layer, bool) {
	if !c.Contains(p) {
		return Layer{}, false
	}
	relative := strings.TrimPrefix(p, c.module+"/")
	if p == c.module {
		relative = "."
	}
	for _, root := range c.roots {
		if relative == root || root != "." && strings.HasPrefix(relative, root+"/") {
			l := c.layers[c.owners[root]]
			// Do not expose the slices used by the validated contract.
			l.Roots = slices.Clone(l.Roots)
			l.Uses = slices.Clone(l.Uses)
			return l, true
		}
	}
	return Layer{}, false
}

// Allows rejects unclassified packages, including imports within an unknown subtree.
func (c *Contract) Allows(from, to string) bool {
	a, ok := c.Layer(from)
	if !ok {
		return false
	}
	b, ok := c.Layer(to)
	return ok && c.allowed[a.Name][b.Name]
}

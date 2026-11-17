// Package cache describes declared cache grants and their inheritance.
package cache

import (
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// EffectiveMode returns the job declaration when present, otherwise the workflow
// declaration. It preserves the selected declaration's source location.
func EffectiveMode(workflow, job *definition.CacheMode) *definition.CacheMode {
	if job != nil {
		return job
	}
	return workflow
}

// Grant distinguishes inheritance from an explicit valid or invalid mode. Its
// fields are private so callers cannot construct a partly declared grant.
type Grant struct {
	present bool
	kind    definition.CacheModeKind
}

func FromDeclaration(mode *definition.CacheMode) Grant {
	if mode == nil {
		return Grant{}
	}
	return Grant{present: true, kind: mode.Kind}
}

func FromMetadata(mode calls.CacheModeDeclaration) Grant {
	kind, declared := mode.Kind()
	return Grant{present: declared, kind: kind}
}

func (g Grant) Present() bool { return g.present }

func (g Grant) Kind() definition.CacheModeKind { return g.kind }

// Override selects an explicit child grant, including an invalid declaration.
// A missing child inherits g; validity is checked separately from inheritance.
func (g Grant) Override(child Grant) Grant {
	if child.present {
		return child
	}
	return g
}

// Capabilities returns the mode's read/write bits and whether they are known.
// Missing and invalid declarations both have unknown capabilities; Present and
// Kind distinguish those states when reporting failures or applying inheritance.
func (g Grant) Capabilities() (uint8, bool) {
	if !g.present {
		return 0, false
	}
	return g.kind.Capabilities()
}

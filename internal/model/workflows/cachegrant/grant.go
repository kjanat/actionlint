// Package cachegrant describes declared cache grants and their inheritance.
package cachegrant

import (
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func EffectiveMode(workflow, job *definition.CacheMode) *definition.CacheMode {
	if job != nil {
		return job
	}
	return workflow
}

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

func (g Grant) Override(child Grant) Grant {
	if child.present {
		return child
	}
	return g
}

// Capabilities reports unknown capabilities for both missing and invalid declarations.
func (g Grant) Capabilities() (uint8, bool) {
	if !g.present {
		return 0, false
	}
	return g.kind.Capabilities()
}

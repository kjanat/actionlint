package scripts

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/checking/scripts -type Unit -output internal/checking/scripts/owned_copy.gen.go

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInputs(scopes []*Scope, units []Unit) ([]*Scope, []Unit) {
	clonedScopes := make([]*Scope, len(scopes))
	byScope := make(map[*Scope]*Scope, len(scopes))
	for i, scope := range scopes {
		clonedScopes[i] = copyValue(scope)
		byScope[scope] = clonedScopes[i]
	}
	var clonedUnits []Unit
	if units != nil {
		clonedUnits = make([]Unit, len(units))
	}
	copier := new(ownedCopyContext)
	for i, unit := range units {
		clonedUnits[i] = copier.copyOwnedUnit(unit)
		clonedUnits[i].Scope = byScope[unit.Scope]
	}
	return clonedScopes, clonedUnits
}

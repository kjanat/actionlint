package calls

import (
	"slices"
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// PermissionLevel is an access level of a single permission scope of the GITHUB_TOKEN.
type PermissionLevel int

const (
	// PermissionLevelNone means the scope grants no access.
	PermissionLevelNone PermissionLevel = iota
	// PermissionLevelRead means the scope grants read access.
	PermissionLevelRead
	// PermissionLevelWrite means the scope grants write access, which includes read access.
	PermissionLevelWrite
)

// String returns the level as it is written in a "permissions:" mapping.
func (l PermissionLevel) String() string {
	switch l {
	case PermissionLevelRead:
		return "read"
	case PermissionLevelWrite:
		return "write"
	case PermissionLevelNone:
	}
	return "none"
}

// PermissionScopeLevels maps a permission scope name to its access level. A scope absent from the
// map is PermissionLevelNone.
type PermissionScopeLevels map[string]PermissionLevel

func parsePermissionLevel(v string) PermissionLevel {
	switch v {
	case "write":
		return PermissionLevelWrite
	case "read":
		return PermissionLevelRead
	default:
		return PermissionLevelNone
	}
}

func clampPermissionLevel(scope string, l PermissionLevel) PermissionLevel {
	vs, ok := allPermissionScopes[scope]
	if !ok {
		return l
	}
	if l == PermissionLevelWrite && !slices.Contains(vs, "write") {
		l = PermissionLevelRead
	}
	if l == PermissionLevelRead && !slices.Contains(vs, "read") {
		l = PermissionLevelNone
	}
	return l
}

// permissionsKind classifies a "permissions:" declaration.
type permissionsKind int

const (
	PermissionsAbsent permissionsKind = iota
	PermissionsInvalid
	PermissionsDeclared
)

// ResolvedPermissions is the outcome of resolving one "permissions:" declaration. levels is only
// meaningful when kind is PermissionsDeclared.
type ResolvedPermissions struct {
	Kind   permissionsKind
	Levels PermissionScopeLevels
}

// permissionsSource is the raw shape of a "permissions:" value. Exactly one of the two forms is
// used: scopes non-nil for the mapping form, all set for the "read-all"/"write-all" scalar form.
type permissionsSource struct {
	all    string
	scopes map[string]string
}

func resolvePermissions(src permissionsSource) ResolvedPermissions {
	ls := PermissionScopeLevels{}

	if src.scopes != nil {
		for s, v := range src.scopes {
			if _, ok := allPermissionScopes[s]; !ok {
				continue
			}
			if l := clampPermissionLevel(s, parsePermissionLevel(v)); l > PermissionLevelNone {
				ls[s] = l
			}
		}
		return ResolvedPermissions{PermissionsDeclared, ls}
	}

	var all PermissionLevel
	switch src.all {
	case "read-all":
		all = PermissionLevelRead
	case "write-all":
		all = PermissionLevelWrite
	default:
		return ResolvedPermissions{Kind: PermissionsInvalid}
	}
	for s := range allPermissionScopes {
		if l := clampPermissionLevel(s, all); l > PermissionLevelNone {
			ls[s] = l
		}
	}
	return ResolvedPermissions{PermissionsDeclared, ls}
}

// ResolvePermissions resolves an immutable parsed permissions declaration.
func ResolvePermissions(p declaration.PermissionsView) ResolvedPermissions {
	if !p.Present() {
		return ResolvedPermissions{Kind: PermissionsAbsent}
	}
	if p.ScopesPresent() {
		scopes := map[string]string{}
		for _, scope := range p.Scopes() {
			scopes[scope.Name().Value()] = strings.ToLower(scope.Value().Value())
		}
		return resolvePermissions(permissionsSource{scopes: scopes})
	}
	if p.All().Present() {
		return resolvePermissions(permissionsSource{all: strings.ToLower(p.All().Value())})
	}
	return ResolvedPermissions{Kind: PermissionsInvalid}
}

// resolvePermissionsModel resolves a parsed "permissions:" section. A nil node means the section is
// absent.
func resolvePermissionsModel(p *workflowlang.Permissions) ResolvedPermissions {
	if p == nil {
		return ResolvedPermissions{Kind: PermissionsAbsent}
	}
	if p.Scopes != nil {
		ss := make(map[string]string, len(p.Scopes))
		for _, s := range p.Scopes {
			ss[s.Name.Value] = strings.ToLower(s.Value.Value)
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	}
	if p.All != nil {
		return resolvePermissions(permissionsSource{all: strings.ToLower(p.All.Value)})
	}
	return ResolvedPermissions{Kind: PermissionsInvalid}
}

// ResolvePermissionsYAML resolves a raw "permissions:" YAML node. A nil node or a node with a zero
// Kind means the section is absent.
func ResolvePermissionsYAML(n *syntax.Node) ResolvedPermissions {
	if n == nil || n.Kind == 0 {
		return ResolvedPermissions{Kind: PermissionsAbsent}
	}
	switch n.Kind {
	case syntax.MappingNode:
		ss := make(map[string]string, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			ss[n.Content[i].Value] = strings.ToLower(n.Content[i+1].Value)
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	case syntax.ScalarNode:
		return resolvePermissions(permissionsSource{all: strings.ToLower(n.Value)})
	case syntax.DocumentNode, syntax.SequenceNode, syntax.AliasNode:
	}
	return ResolvedPermissions{Kind: PermissionsInvalid}
}

// DefaultPermissionLevels returns the levels GitHub grants when neither the calling job nor the
// caller workflow declares "permissions:". The repository's "Workflow permissions" setting decides
// this and actionlint cannot read it, so the assumption is selected by configuration.
func DefaultPermissionLevels(permissive bool) PermissionScopeLevels {
	if !permissive {
		return PermissionScopeLevels{
			"contents": PermissionLevelRead,
			"packages": PermissionLevelRead,
		}
	}
	ls := PermissionScopeLevels{}
	for s := range allPermissionScopes {
		if s == "id-token" {
			continue
		}
		if l := clampPermissionLevel(s, PermissionLevelWrite); l > PermissionLevelNone {
			ls[s] = l
		}
	}
	return ls
}

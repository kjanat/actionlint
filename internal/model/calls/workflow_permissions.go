package calls

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/permissions"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

var allPermissionScopes = permissions.Scopes()

type PermissionLevel int

const (
	PermissionLevelNone PermissionLevel = iota
	PermissionLevelRead
	PermissionLevelWrite
)

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

// PermissionScopeLevels treats a scope absent from the map as PermissionLevelNone.
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

type permissionsKind int

const (
	PermissionsAbsent permissionsKind = iota
	PermissionsInvalid
	PermissionsDeclared
)

// ResolvedPermissions levels are only meaningful when kind is PermissionsDeclared.
type ResolvedPermissions struct {
	Kind   permissionsKind
	Levels PermissionScopeLevels
}

// EffectivePermissions stops inheritance at an explicit invalid declaration. The returned map
// does not share storage with defaults or the published declarations.
func EffectivePermissions(workflow, job *definition.Permissions, defaults ResolvedPermissions) ResolvedPermissions {
	if job != nil {
		return ResolvePermissions(job)
	}
	if workflow != nil {
		return ResolvePermissions(workflow)
	}
	return ResolvedPermissions{Kind: defaults.Kind, Levels: maps.Clone(defaults.Levels)}
}

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

func ResolvePermissions(p *definition.Permissions) ResolvedPermissions {
	if p == nil {
		return ResolvedPermissions{Kind: PermissionsAbsent}
	}
	if p.Scopes != nil {
		ss := make(map[string]string, len(p.Scopes))
		for _, s := range p.Scopes {
			if s != nil {
				ss[s.Name.Text()] = strings.ToLower(s.Value.Text())
			}
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	}
	if p.All != nil {
		return resolvePermissions(permissionsSource{all: strings.ToLower(p.All.Value)})
	}
	return ResolvedPermissions{Kind: PermissionsInvalid}
}

func ResolvePermissionsYAML(n *codec.Node) ResolvedPermissions {
	if n == nil || n.Kind == 0 {
		return ResolvedPermissions{Kind: PermissionsAbsent}
	}
	switch n.Kind {
	case codec.MappingNode:
		ss := make(map[string]string, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			ss[n.Content[i].Value] = strings.ToLower(n.Content[i+1].Value)
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	case codec.ScalarNode:
		return resolvePermissions(permissionsSource{all: strings.ToLower(n.Value)})
	case codec.DocumentNode, codec.SequenceNode, codec.AliasNode:
	}
	return ResolvedPermissions{Kind: PermissionsInvalid}
}

// DefaultPermissionLevels takes the repository's "Workflow permissions" setting from configuration.
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

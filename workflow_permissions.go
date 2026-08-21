package actionlint

import (
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
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
	default:
		return "none"
	}
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
	permissionsAbsent permissionsKind = iota
	permissionsInvalid
	permissionsDeclared
)

// resolvedPermissions is the outcome of resolving one "permissions:" declaration. levels is only
// meaningful when kind is permissionsDeclared.
type resolvedPermissions struct {
	kind   permissionsKind
	levels PermissionScopeLevels
}

// permissionsSource is the raw shape of a "permissions:" value. Exactly one of the two forms is
// used: scopes non-nil for the mapping form, all set for the "read-all"/"write-all" scalar form.
type permissionsSource struct {
	all    string
	scopes map[string]string
}

func resolvePermissions(src permissionsSource) resolvedPermissions {
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
		return resolvedPermissions{permissionsDeclared, ls}
	}

	var all PermissionLevel
	switch src.all {
	case "read-all":
		all = PermissionLevelRead
	case "write-all":
		all = PermissionLevelWrite
	default:
		return resolvedPermissions{kind: permissionsInvalid}
	}
	for s := range allPermissionScopes {
		if l := clampPermissionLevel(s, all); l > PermissionLevelNone {
			ls[s] = l
		}
	}
	return resolvedPermissions{permissionsDeclared, ls}
}

// resolvePermissionsAST resolves a parsed "permissions:" section. A nil node means the section is
// absent.
func resolvePermissionsAST(p *Permissions) resolvedPermissions {
	if p == nil {
		return resolvedPermissions{kind: permissionsAbsent}
	}
	if p.Scopes != nil {
		ss := make(map[string]string, len(p.Scopes))
		for n, s := range p.Scopes {
			ss[n] = strings.ToLower(s.Value.Value)
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	}
	if p.All != nil {
		return resolvePermissions(permissionsSource{all: strings.ToLower(p.All.Value)})
	}
	return resolvedPermissions{kind: permissionsInvalid}
}

// resolvePermissionsYAML resolves a raw "permissions:" YAML node. A nil node or a node with a zero
// Kind means the section is absent.
func resolvePermissionsYAML(n *yaml.Node) resolvedPermissions {
	if n == nil || n.Kind == 0 {
		return resolvedPermissions{kind: permissionsAbsent}
	}
	switch n.Kind {
	case yaml.MappingNode:
		ss := make(map[string]string, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			ss[strings.ToLower(n.Content[i].Value)] = strings.ToLower(n.Content[i+1].Value)
		}
		return resolvePermissions(permissionsSource{scopes: ss})
	case yaml.ScalarNode:
		return resolvePermissions(permissionsSource{all: strings.ToLower(n.Value)})
	default:
		return resolvedPermissions{kind: permissionsInvalid}
	}
}

// defaultPermissionLevels returns the levels GitHub grants when neither the calling job nor the
// caller workflow declares "permissions:". The repository's "Workflow permissions" setting decides
// this and actionlint cannot read it, so the assumption is selected by configuration.
func defaultPermissionLevels(a DefaultPermissionsAssumption) PermissionScopeLevels {
	if a != DefaultPermissionsAssumptionPermissive {
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

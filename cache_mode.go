package actionlint

import "go.yaml.in/yaml/v4"

// CacheModeKind identifies the cache access granted to a workflow or job.
type CacheModeKind uint8

const (
	// CacheModeInvalid represents an invalid declaration, distinct from an omitted one.
	CacheModeInvalid CacheModeKind = iota
	// CacheModeNone prevents restores and saves.
	CacheModeNone
	// CacheModeRead allows restores only.
	CacheModeRead
	// CacheModeWrite allows restores and saves.
	CacheModeWrite
	// CacheModeWriteOnly allows saves only.
	CacheModeWriteOnly
)

// String returns the workflow spelling of the cache mode.
func (k CacheModeKind) String() string {
	switch k {
	case CacheModeNone:
		return "none"
	case CacheModeRead:
		return "read"
	case CacheModeWrite:
		return "write"
	case CacheModeWriteOnly:
		return "write-only"
	default:
		return "invalid"
	}
}

// CacheMode is an explicit cache-mode declaration. An omitted declaration is a nil pointer.
type CacheMode struct {
	Kind CacheModeKind
	Pos  *Pos
}

func (m *CacheMode) capabilities() (uint8, bool) {
	if m != nil {
		switch m.Kind {
		case CacheModeNone:
			return 0, true
		case CacheModeRead:
			return 1, true
		case CacheModeWriteOnly:
			return 2, true
		case CacheModeWrite:
			return 3, true
		}
	}
	return 0, false
}

func effectiveCacheMode(workflow, job *CacheMode) *CacheMode {
	if job != nil {
		return job
	}
	return workflow
}

func cacheModeFromYAML(n *yaml.Node) *CacheMode {
	if n == nil || n.Kind == 0 {
		return nil
	}
	m := &CacheMode{Pos: posAt(n)}
	if n.Kind == yaml.ScalarNode && n.Tag == yamlTagStr {
		switch n.Value {
		case "none":
			m.Kind = CacheModeNone
		case "read":
			m.Kind = CacheModeRead
		case "write":
			m.Kind = CacheModeWrite
		case "write-only":
			m.Kind = CacheModeWriteOnly
		}
	}
	return m
}

func (p *parser) parseCacheMode(n *yaml.Node) *CacheMode {
	m := cacheModeFromYAML(n)
	if n.Kind != yaml.ScalarNode || n.Tag != yamlTagStr {
		p.typeErrorf(n, "expected string for \"cache-mode\" but found %s node with %q tag", nodeKindName(n.Kind), n.Tag)
	} else if m.Kind == CacheModeInvalid {
		p.typeErrorf(n, "%q is invalid for \"cache-mode\". expected one of \"read\", \"write\", \"write-only\", \"none\"", n.Value)
	}
	return m
}

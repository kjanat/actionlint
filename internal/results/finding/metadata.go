package finding

type MetadataKey struct {
	Path, Message string
	Line, Column  int
}

type MetadataIdentity struct {
	Key         MetadataKey
	Visit       uint64
	Declaration bool
}

// FirstVisibleMetadata preserves the first visible metadata invocation when filtering
// reorders findings by source file, independently of map iteration order.
func FirstVisibleMetadata(findings []*Finding) []*Finding {
	type key struct {
		finding        MetadataKey
		kind, severity string
	}
	first := make(map[key]*Finding)
	for _, finding := range findings {
		if identity := finding.MetadataIdentity; identity != nil && finding.Rule == "action" {
			k := key{identity.Key, finding.Rule, finding.Severity}
			if previous := first[k]; previous == nil || identity.Visit < previous.MetadataIdentity.Visit {
				first[k] = finding
			}
		}
	}
	filtered := findings[:0]
	for _, finding := range findings {
		if identity := finding.MetadataIdentity; identity != nil && finding.Rule == "action" && first[key{identity.Key, finding.Rule, finding.Severity}] != finding {
			continue
		}
		filtered = append(filtered, finding)
	}
	clear(findings[len(filtered):])
	return filtered
}

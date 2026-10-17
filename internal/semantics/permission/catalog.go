package permission

import "slices"

var scopes = map[string][]string{
	"actions":              {"read", "write", "none"},
	"artifact-metadata":    {"read", "write", "none"},
	"attestations":         {"read", "write", "none"},
	"checks":               {"read", "write", "none"},
	"code-quality":         {"read", "write", "none"},
	"contents":             {"read", "write", "none"},
	"copilot-requests":     {"write", "none"},
	"deployments":          {"read", "write", "none"},
	"discussions":          {"read", "write", "none"},
	"drives":               {"read", "write", "none"},
	"id-token":             {"write", "none"},
	"issues":               {"read", "write", "none"},
	"models":               {"read", "none"},
	"packages":             {"read", "write", "none"},
	"pages":                {"read", "write", "none"},
	"pull-requests":        {"read", "write", "none"},
	"repository-projects":  {"read", "write", "none"},
	"security-events":      {"read", "write", "none"},
	"statuses":             {"read", "write", "none"},
	"vulnerability-alerts": {"read", "none"},
}

func Scopes() map[string][]string {
	result := make(map[string][]string, len(scopes))
	for name, levels := range scopes {
		result[name] = slices.Clone(levels)
	}
	return result
}

package calls

import (
	"testing"

	workflow "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestMetadataCloneDetachesNestedOwnership(t *testing.T) {
	t.Parallel()
	mode := DeclaredCacheMode(workflow.CacheModeWrite)
	original := &Metadata{Inputs: Inputs{"a": {Name: "a"}}, Outputs: Outputs{"b": {Name: "b"}}, Secrets: Secrets{"c": {Name: "c"}}, JobPermissions: map[string]PermissionScopeLevels{"job": {"contents": PermissionLevelRead}}, JobCacheAccess: map[string]CacheAccess{"job": {Mode: mode, Uses: "./callee", SourceUses: "./callee.yml", Operations: []string{"actions/cache"}}}}
	clone := original.Clone()
	delete(clone.Inputs, "a")
	delete(clone.Outputs, "b")
	delete(clone.Secrets, "c")
	clone.JobPermissions["job"]["contents"] = PermissionLevelWrite
	access := clone.JobCacheAccess["job"]
	access.Operations[0] = "changed"
	clone.JobCacheAccess["job"] = access
	if len(original.Inputs) != 1 || len(original.Outputs) != 1 || len(original.Secrets) != 1 || original.JobPermissions["job"]["contents"] != PermissionLevelRead || original.JobCacheAccess["job"].Operations[0] != "actions/cache" || !access.Mode.Equal(mode) || access.Uses != "./callee" || access.SourceUses != "./callee.yml" {
		t.Fatalf("clone changed source or lost scalar data: source=%+v clone=%+v", original, clone)
	}
	if (*Metadata)(nil).Clone() != nil {
		t.Fatal("nil metadata changed")
	}
}

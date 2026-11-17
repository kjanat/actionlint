package calls

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestEffectivePermissionsPrecedenceAndDetachedDefaults(t *testing.T) {
	t.Parallel()
	view := func(permissions *definition.Permissions) *definition.Permissions {
		return workflows.Bind("permissions", "", &definition.Workflow{Permissions: permissions}, false, "").Workflow().Permissions
	}
	read := view(&definition.Permissions{All: &definition.String{Value: "read-all"}})
	empty := view(&definition.Permissions{Scopes: map[string]*definition.PermissionScope{}})
	invalid := view(&definition.Permissions{All: &definition.String{Value: "invalid"}})
	defaults := ResolvedPermissions{Kind: PermissionsDeclared, Levels: PermissionScopeLevels{"contents": PermissionLevelWrite}}
	for _, tc := range []struct {
		name          string
		workflow, job *definition.Permissions
		kind          permissionsKind
		contents      PermissionLevel
	}{
		{"repository default", nil, nil, PermissionsDeclared, PermissionLevelWrite},
		{"workflow override", read, nil, PermissionsDeclared, PermissionLevelRead},
		{"explicit empty job", read, empty, PermissionsDeclared, PermissionLevelNone},
		{"invalid job stops inheritance", read, invalid, PermissionsInvalid, PermissionLevelNone},
		{"invalid workflow stops inheritance", invalid, nil, PermissionsInvalid, PermissionLevelNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := EffectivePermissions(tc.workflow, tc.job, defaults)
			if result.Kind != tc.kind || result.Levels["contents"] != tc.contents {
				t.Fatalf("effective permissions=%+v", result)
			}
			if result.Levels != nil {
				result.Levels["contents"] = PermissionLevelNone
			}
			if defaults.Levels["contents"] != PermissionLevelWrite {
				t.Fatal("result mutation changed repository defaults")
			}
		})
	}
	if result := EffectivePermissions(nil, nil, ResolvedPermissions{}); result.Kind != PermissionsAbsent {
		t.Fatalf("unknown defaults became an explicit permission grant: %+v", result)
	}
}

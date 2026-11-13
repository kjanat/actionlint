package calls

import (
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestEffectivePermissionsPrecedenceAndDetachedDefaults(t *testing.T) {
	t.Parallel()
	view := func(permissions *workflowlang.Permissions) declaration.PermissionsView {
		return declaration.Publish("", &workflowlang.Workflow{Permissions: permissions}, false, "").Bind("permissions").Workflow().Permissions()
	}
	read := view(&workflowlang.Permissions{All: &workflowlang.String{Value: "read-all"}})
	empty := view(&workflowlang.Permissions{Scopes: map[string]*workflowlang.PermissionScope{}})
	invalid := view(&workflowlang.Permissions{All: &workflowlang.String{Value: "invalid"}})
	defaults := ResolvedPermissions{Kind: PermissionsDeclared, Levels: PermissionScopeLevels{"contents": PermissionLevelWrite}}
	for _, tc := range []struct {
		name          string
		workflow, job declaration.PermissionsView
		kind          permissionsKind
		contents      PermissionLevel
	}{
		{"repository default", declaration.PermissionsView{}, declaration.PermissionsView{}, PermissionsDeclared, PermissionLevelWrite},
		{"workflow override", read, declaration.PermissionsView{}, PermissionsDeclared, PermissionLevelRead},
		{"explicit empty job", read, empty, PermissionsDeclared, PermissionLevelNone},
		{"invalid job stops inheritance", read, invalid, PermissionsInvalid, PermissionLevelNone},
		{"invalid workflow stops inheritance", invalid, declaration.PermissionsView{}, PermissionsInvalid, PermissionLevelNone},
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
	if result := EffectivePermissions(declaration.PermissionsView{}, declaration.PermissionsView{}, ResolvedPermissions{}); result.Kind != PermissionsAbsent {
		t.Fatalf("unknown defaults became an explicit permission grant: %+v", result)
	}
}

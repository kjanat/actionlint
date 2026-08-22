package actionlint

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

func TestPermissionLevelString(t *testing.T) {
	tests := []struct {
		level PermissionLevel
		want  string
	}{
		{PermissionLevelNone, "none"},
		{PermissionLevelRead, "read"},
		{PermissionLevelWrite, "write"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if have := tc.level.String(); have != tc.want {
				t.Fatalf("wanted %q but have %q", tc.want, have)
			}
		})
	}
}

func TestPermissionLevelClamp(t *testing.T) {
	tests := []struct {
		what  string
		scope string
		level PermissionLevel
		want  PermissionLevel
	}{
		{"id-token read is not available", "id-token", PermissionLevelRead, PermissionLevelNone},
		{"id-token write is available", "id-token", PermissionLevelWrite, PermissionLevelWrite},
		{"models write falls back to read", "models", PermissionLevelWrite, PermissionLevelRead},
		{"vulnerability-alerts write falls back to read", "vulnerability-alerts", PermissionLevelWrite, PermissionLevelRead},
		{"copilot-requests read is not available", "copilot-requests", PermissionLevelRead, PermissionLevelNone},
		{"contents write is available", "contents", PermissionLevelWrite, PermissionLevelWrite},
		{"contents read is available", "contents", PermissionLevelRead, PermissionLevelRead},
		{"none is never raised", "contents", PermissionLevelNone, PermissionLevelNone},
		{"unknown scope is untouched", "this-scope-does-not-exist", PermissionLevelWrite, PermissionLevelWrite},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			if have := clampPermissionLevel(tc.scope, tc.level); have != tc.want {
				t.Fatalf("wanted %v but have %v for scope %q", tc.want, have, tc.scope)
			}
		})
	}
}

func TestResolvePermissionsShapes(t *testing.T) {
	readAll := PermissionScopeLevels{}
	writeAll := PermissionScopeLevels{}
	for s, vs := range allPermissionScopes {
		if slices.Contains(vs, "read") {
			readAll[s] = PermissionLevelRead
		}
		if slices.Contains(vs, "write") {
			writeAll[s] = PermissionLevelWrite
		} else {
			writeAll[s] = PermissionLevelRead
		}
	}

	tests := []struct {
		what   string
		src    permissionsSource
		kind   permissionsKind
		levels PermissionScopeLevels
	}{
		{
			what:   "mapping",
			src:    permissionsSource{scopes: map[string]string{"contents": "read", "pull-requests": "write"}},
			kind:   permissionsDeclared,
			levels: PermissionScopeLevels{"contents": PermissionLevelRead, "pull-requests": PermissionLevelWrite},
		},
		{
			what:   "empty mapping",
			src:    permissionsSource{scopes: map[string]string{}},
			kind:   permissionsDeclared,
			levels: PermissionScopeLevels{},
		},
		{
			what:   "none is not recorded",
			src:    permissionsSource{scopes: map[string]string{"contents": "none"}},
			kind:   permissionsDeclared,
			levels: PermissionScopeLevels{},
		},
		{
			what:   "unknown scope is dropped",
			src:    permissionsSource{scopes: map[string]string{"contents": "read", "this-scope-does-not-exist": "write"}},
			kind:   permissionsDeclared,
			levels: PermissionScopeLevels{"contents": PermissionLevelRead},
		},
		{
			what:   "clamped in mapping",
			src:    permissionsSource{scopes: map[string]string{"id-token": "read", "models": "write"}},
			kind:   permissionsDeclared,
			levels: PermissionScopeLevels{"models": PermissionLevelRead},
		},
		{
			what:   "read-all",
			src:    permissionsSource{all: "read-all"},
			kind:   permissionsDeclared,
			levels: readAll,
		},
		{
			what:   "write-all",
			src:    permissionsSource{all: "write-all"},
			kind:   permissionsDeclared,
			levels: writeAll,
		},
		{
			what: "unknown scalar",
			src:  permissionsSource{all: "foo"},
			kind: permissionsInvalid,
		},
		{
			what: "empty scalar",
			src:  permissionsSource{},
			kind: permissionsInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have := resolvePermissions(tc.src)
			if have.kind != tc.kind {
				t.Fatalf("wanted kind %v but have %v", tc.kind, have.kind)
			}
			if diff := cmp.Diff(tc.levels, have.levels); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestResolvePermissionsASTAndYAMLAgree(t *testing.T) {
	tests := []string{
		"permissions:",
		"permissions: ~",
		"permissions: null",
		"permissions: {}",
		"permissions: read-all",
		"permissions: write-all",
		"permissions: bogus",
		"permissions:\n  contents: read",
		"permissions:\n  contents: read\n  id-token: write",
		"permissions:\n  Contents: Read",
		"permissions:\n  this-scope-does-not-exist: read",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			src := "on: push\n" + tc + "\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n"

			w, _ := Parse([]byte(src))
			if w == nil {
				t.Fatal("workflow was not parsed")
			}
			fromAST := resolvePermissionsAST(w.Permissions)

			var n struct {
				Permissions yaml.Node `yaml:"permissions"`
			}
			if err := yaml.Unmarshal([]byte(src), &n); err != nil {
				t.Fatal(err)
			}
			fromYAML := resolvePermissionsYAML(&n.Permissions)

			if fromAST.kind != fromYAML.kind {
				t.Fatalf("kind from AST is %v but kind from YAML is %v", fromAST.kind, fromYAML.kind)
			}
			if diff := cmp.Diff(fromAST.levels, fromYAML.levels); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestResolvePermissionsScopeNamesAreCaseSensitive(t *testing.T) {
	src := []byte("on: push\npermissions:\n  Contents: read\n  issues: Write\n")
	w, _ := Parse(src)
	if w == nil {
		t.Fatal("workflow was not parsed")
	}

	var n struct {
		Permissions yaml.Node `yaml:"permissions"`
	}
	if err := yaml.Unmarshal(src, &n); err != nil {
		t.Fatal(err)
	}

	want := PermissionScopeLevels{"issues": PermissionLevelWrite}
	for route, have := range map[string]resolvedPermissions{
		"AST":  resolvePermissionsAST(w.Permissions),
		"YAML": resolvePermissionsYAML(&n.Permissions),
	} {
		if have.kind != permissionsDeclared {
			t.Errorf("%s resolver returned kind %v", route, have.kind)
		}
		if diff := cmp.Diff(want, have.levels); diff != "" {
			t.Errorf("%s resolver (-want +have):\n%s", route, diff)
		}
	}
}

func TestResolvePermissionsAbsent(t *testing.T) {
	if have := resolvePermissionsAST(nil); have.kind != permissionsAbsent {
		t.Errorf("nil AST node should be absent but got %v", have.kind)
	}
	if have := resolvePermissionsYAML(nil); have.kind != permissionsAbsent {
		t.Errorf("nil YAML node should be absent but got %v", have.kind)
	}
	if have := resolvePermissionsYAML(&yaml.Node{}); have.kind != permissionsAbsent {
		t.Errorf("zero YAML node should be absent but got %v", have.kind)
	}
	if have := resolvePermissionsYAML(&yaml.Node{Kind: yaml.SequenceNode}); have.kind != permissionsInvalid {
		t.Errorf("sequence YAML node should be invalid but got %v", have.kind)
	}
}

func TestDefaultPermissionLevels(t *testing.T) {
	restricted := PermissionScopeLevels{
		"contents": PermissionLevelRead,
		"packages": PermissionLevelRead,
	}

	permissive := PermissionScopeLevels{}
	for s, vs := range allPermissionScopes {
		if s == "id-token" {
			continue
		}
		if slices.Contains(vs, "write") {
			permissive[s] = PermissionLevelWrite
		} else {
			permissive[s] = PermissionLevelRead
		}
	}

	tests := []struct {
		what string
		a    DefaultPermissionsAssumption
		want PermissionScopeLevels
	}{
		{"unset", DefaultPermissionsAssumptionUnset, restricted},
		{"restricted", DefaultPermissionsAssumptionRestricted, restricted},
		{"permissive", DefaultPermissionsAssumptionPermissive, permissive},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, defaultPermissionLevels(tc.a)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

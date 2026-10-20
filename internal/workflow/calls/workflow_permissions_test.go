package calls

import (
	"slices"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"

	"actionlint.kjanat.dev/internal/yaml"
	"github.com/google/go-cmp/cmp"
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
		Kind   permissionsKind
		Levels PermissionScopeLevels
	}{
		{
			what:   "mapping",
			src:    permissionsSource{scopes: map[string]string{"contents": "read", "pull-requests": "write"}},
			Kind:   PermissionsDeclared,
			Levels: PermissionScopeLevels{"contents": PermissionLevelRead, "pull-requests": PermissionLevelWrite},
		},
		{
			what:   "empty mapping",
			src:    permissionsSource{scopes: map[string]string{}},
			Kind:   PermissionsDeclared,
			Levels: PermissionScopeLevels{},
		},
		{
			what:   "none is not recorded",
			src:    permissionsSource{scopes: map[string]string{"contents": "none"}},
			Kind:   PermissionsDeclared,
			Levels: PermissionScopeLevels{},
		},
		{
			what:   "unknown scope is dropped",
			src:    permissionsSource{scopes: map[string]string{"contents": "read", "this-scope-does-not-exist": "write"}},
			Kind:   PermissionsDeclared,
			Levels: PermissionScopeLevels{"contents": PermissionLevelRead},
		},
		{
			what:   "clamped in mapping",
			src:    permissionsSource{scopes: map[string]string{"id-token": "read", "models": "write"}},
			Kind:   PermissionsDeclared,
			Levels: PermissionScopeLevels{"models": PermissionLevelRead},
		},
		{
			what:   "read-all",
			src:    permissionsSource{all: "read-all"},
			Kind:   PermissionsDeclared,
			Levels: readAll,
		},
		{
			what:   "write-all",
			src:    permissionsSource{all: "write-all"},
			Kind:   PermissionsDeclared,
			Levels: writeAll,
		},
		{
			what: "unknown scalar",
			src:  permissionsSource{all: "foo"},
			Kind: PermissionsInvalid,
		},
		{
			what: "empty scalar",
			src:  permissionsSource{},
			Kind: PermissionsInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have := resolvePermissions(tc.src)
			if have.Kind != tc.Kind {
				t.Fatalf("wanted kind %v but have %v", tc.Kind, have.Kind)
			}
			if diff := cmp.Diff(tc.Levels, have.Levels); diff != "" {
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
		`permissions:
  contents: read
  id-token: write`,
		"permissions:\n  Contents: Read",
		"permissions:\n  this-scope-does-not-exist: read",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			src := "on: push\n" + tc + `
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`

			w, _ := workflowlang.Parse([]byte(src))
			if w == nil {
				t.Fatal("workflow was not parsed")
			}
			fromAST := ResolvePermissionsAST(w.Permissions)

			var n struct {
				Permissions yaml.Node `yaml:"permissions"`
			}
			if err := yaml.Unmarshal([]byte(src), &n); err != nil {
				t.Fatal(err)
			}
			fromYAML := ResolvePermissionsYAML(&n.Permissions)

			if fromAST.Kind != fromYAML.Kind {
				t.Fatalf("kind from AST is %v but kind from YAML is %v", fromAST.Kind, fromYAML.Kind)
			}
			if diff := cmp.Diff(fromAST.Levels, fromYAML.Levels); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestResolvePermissionsScopeNamesAreCaseSensitive(t *testing.T) {
	src := []byte(`on: push
permissions:
  Contents: read
  issues: Write
`)
	w, _ := workflowlang.Parse(src)
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
	for route, have := range map[string]ResolvedPermissions{
		"AST":  ResolvePermissionsAST(w.Permissions),
		"YAML": ResolvePermissionsYAML(&n.Permissions),
	} {
		if have.Kind != PermissionsDeclared {
			t.Errorf("%s resolver returned kind %v", route, have.Kind)
		}
		if diff := cmp.Diff(want, have.Levels); diff != "" {
			t.Errorf("%s resolver (-want +have):\n%s", route, diff)
		}
	}
}

func TestResolvePermissionsAbsent(t *testing.T) {
	if have := ResolvePermissionsAST(nil); have.Kind != PermissionsAbsent {
		t.Errorf("nil AST node should be absent but got %v", have.Kind)
	}
	if have := ResolvePermissionsYAML(nil); have.Kind != PermissionsAbsent {
		t.Errorf("nil YAML node should be absent but got %v", have.Kind)
	}
	if have := ResolvePermissionsYAML(&yaml.Node{}); have.Kind != PermissionsAbsent {
		t.Errorf("zero YAML node should be absent but got %v", have.Kind)
	}
	if have := ResolvePermissionsYAML(&yaml.Node{Kind: yaml.SequenceNode}); have.Kind != PermissionsInvalid {
		t.Errorf("sequence YAML node should be invalid but got %v", have.Kind)
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
		a    bool
		want PermissionScopeLevels
	}{
		{"unset", false, restricted},
		{"restricted", false, restricted},
		{"permissive", true, permissive},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, DefaultPermissionLevels(tc.a)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowCallMetadataFailureSpelling(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		for _, prefix := range []string{"./", "$/", "$//"} {
			t.Run(fmt.Sprintf("malformed=%v/prefix=%s", malformed, prefix), func(t *testing.T) {
				root := t.TempDir()
				if malformed {
					if err := os.WriteFile(filepath.Join(root, "broken.yaml"), []byte("on: ["), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				proj := workspace.Identity(root)
				cache := calls.NewCache(proj.RootDir(), root, nil)
				l, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root})
				if err != nil {
					t.Fatal(err)
				}
				uses := prefix + "broken.yaml"
				source := []byte(`on: push
jobs:
  call:
    uses: ` + uses + `
    with: {arg: '${{ github.ref }}'}
  consumer:
    needs: call
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ needs.call.outputs.result }}'
`)
				for _, pass := range []string{"cold cache", "warm cache"} {
					errs, err := l.check("caller.yaml", source, proj, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					if len(errs) != 1 || errs[0].Start.Line != 4 || errs[0].Start.Column != 11 {
						t.Fatalf("%s: want one failure at the uses value, got %v", pass, errs)
					}
					if !strings.Contains(errs[0].Message, fmt.Sprintf("%q", uses)) {
						t.Fatalf("%s: original reference lost: %v", pass, errs)
					}
				}
				errs, err := l.Lint("caller.yaml", source, proj)
				if err != nil {
					t.Fatal(err)
				}
				if len(errs) != 1 || errs[0].Start.Line != 4 || errs[0].Start.Column != 11 || !strings.Contains(errs[0].Message, fmt.Sprintf("%q", uses)) {
					t.Fatalf("Lint: want one failure using the source reference, got %v", errs)
				}
				// Rendering a caller's spelling must not alter the cached error.
				_, err = cache.FindMetadata("./broken.yaml")
				if err == nil || !strings.Contains(err.Error(), `"./broken.yaml"`) {
					t.Fatalf("cache no longer retains its lookup failure: %v", err)
				}
			})
		}
	}
}

func TestWorkflowCallInvalidJobKeyDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, leaf string
	}{
		{"cache mode", "CACHE-MODE", "write", ""},
		{"permissions", "PERMISSIONS", "{contents: write}", ""},
		{"missing unintended callee", "USES", "./leaf.yaml", ""},
		{"malformed unintended callee", "USES", "./leaf.yaml", "on: ["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			caller := []byte(`on: push
cache-mode: read
permissions: {contents: read}
jobs:
  call:
    uses: ./callee.yaml
`)
			callee := []byte(`on: workflow_call
jobs:
  build:
    ` + tc.key + ": " + tc.value + `
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`)
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.leaf != "" {
				if err := os.WriteFile(filepath.Join(root, "leaf.yaml"), []byte(tc.leaf), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			project := workspace.Identity(root)
			linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics := func(diagnostics []*findingmodel.Finding, includeCallee bool) {
				t.Helper()
				if !includeCallee {
					if len(diagnostics) != 0 {
						t.Fatalf("invalid callee key affected caller: %v", diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 || diagnostics[0].Rule != "syntax-check" || diagnostics[0].Start.Line != 4 || diagnostics[0].Start.Column != 5 || filepath.Base(diagnostics[0].Path) != "callee.yaml" || !strings.Contains(diagnostics[0].Message, fmt.Sprintf("unexpected key %q", tc.key)) {
					t.Fatalf("expected only the invalid key at callee.yaml:4:5, got %v", diagnostics)
				}
			}
			for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
				cache := calls.NewCache(project.RootDir(), root, nil)
				for range 2 {
					for _, name := range order {
						source := caller
						if name == "callee.yaml" {
							source = callee
						}
						diagnostics, err := linter.check(name, source, project, nil, nil, cache)
						if err != nil {
							t.Fatal(err)
						}
						checkDiagnostics(diagnostics, name == "callee.yaml")
					}
				}
				paths := []string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}
				diagnostics, err := linter.LintFiles(paths, project)
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(diagnostics, true)
			}
		})
	}
}

func TestRuleWorkflowCallCheckWorkflowCallUsesFormat(t *testing.T) {
	tests := []struct {
		uses string
		ok   bool
	}{
		{"owner/repo/x.yml@ref", true},
		{"owner/repo/x.yml@@", true},
		{"owner/repo/x.yml@release/v1", true},
		{"./path/to/x.yml", true},
		{"$/path/to/x.yml", true},
		{"$//path/to/x.yml", true},
		{"${{ env.FOO }}", true},
		{"./path/to/x.yml@ref", false},
		{"$/path/to/x.yml@ref", false},
		{"/path/to/x.yml@ref", false},
		{"./", false},
		{"$/", false},
		{"$//", false},
		{"$///", false},
		{".", false},
		{"owner/x.yml@ref", false},
		{"owner/repo@ref", false},
		{"owner/repo/x.yml", false},
		{"/repo/x.yml@ref", false},
		{"owner//x.yml@ref", false},
		{"owner/repo/@ref", false},
		{"owner/repo/x.yml@", false},
	}

	for _, tc := range tests {
		t.Run(tc.uses, func(t *testing.T) {
			c := calls.NewCache("", "", nil)
			r := NewRuleWorkflowCall("", c)
			j := &workflowlang.Job{
				WorkflowCall: &workflowlang.WorkflowCall{
					Uses: &workflowlang.String{
						Value: tc.uses,
						Pos:   &workflowlang.Pos{},
					},
				},
			}
			err := r.VisitJobPre(j)
			if err != nil {
				t.Fatal(err)
			}
			errs := r.Errs()
			if tc.ok && len(errs) > 0 {
				t.Fatalf("Error occurred: %v", errs)
			}
			if !tc.ok {
				if len(errs) > 2 || len(errs) == 0 {
					t.Fatalf("Wanted one error but have: %v", errs)
				}
			}
		})
	}
}

func TestRuleWorkflowCallNestedWorkflowCalls(t *testing.T) {
	w := &workflowlang.Workflow{
		On: []workflowlang.Event{
			&workflowlang.WorkflowCallEvent{
				Pos: &workflowlang.Pos{},
			},
		},
	}

	j := &workflowlang.Job{
		WorkflowCall: &workflowlang.WorkflowCall{
			Uses: &workflowlang.String{
				Value: "o/r/w.yaml@r",
				Pos:   &workflowlang.Pos{},
			},
		},
	}

	c := calls.NewCache("", "", nil)
	r := NewRuleWorkflowCall("", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	if err := r.VisitJobPre(j); err != nil {
		t.Fatal(err)
	}
	errs := r.Errs()

	if len(errs) > 0 {
		t.Fatal("unexpected errors:", errs)
	}
}

func TestRuleWorkflowCallWriteEventNodeToMetadataCache(t *testing.T) {
	s := func(v string) *workflowlang.String {
		return &workflowlang.String{Value: v, Pos: &workflowlang.Pos{}}
	}
	w := &workflowlang.Workflow{
		On: []workflowlang.Event{
			&workflowlang.WorkflowCallEvent{
				Inputs: []*workflowlang.WorkflowCallEventInput{
					{
						Name: s("input1"),
						Type: workflowlang.WorkflowCallEventInputTypeString,
						ID:   "input1",
					},
				},
				Outputs: map[string]*workflowlang.WorkflowCallEventOutput{
					"output1": {Name: s("output1")},
				},
				Secrets: map[string]*workflowlang.WorkflowCallEventSecret{
					"secret1": {Name: s("secret1")},
				},
				Pos: &workflowlang.Pos{},
			},
		},
	}

	cwd := filepath.Join("path", "to", "project")
	c := calls.NewCache(cwd, cwd, nil)
	r := NewRuleWorkflowCall("test-workflow.yaml", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	errs := r.Errs()
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	m, _, ok := c.Lookup("./test-workflow.yaml")
	if !ok {
		t.Fatal("no metadata was created")
	}

	want := &calls.Metadata{
		Inputs: calls.Inputs{
			"input1": {Name: "input1", Required: false, Type: exprlang.StringType{}},
		},
		Outputs: calls.Outputs{
			"output1": {Name: "output1"},
		},
		Secrets: calls.Secrets{
			"secret1": {Name: "secret1", Required: false},
		},
	}

	if diff := cmp.Diff(want, m); diff != "" {
		t.Fatal(diff)
	}
}

func testParseWorkflowPermissions(t *testing.T, src string) *workflowlang.Permissions {
	t.Helper()
	if src == "" {
		return nil
	}
	b := []byte("on: push\n" + src + `
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`)
	w, errs := Parse(b)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}
	return w.Permissions
}

func TestRuleWorkflowCallPermissionsCachePathParity(t *testing.T) {
	root := filepath.Join("testdata", "reusable_workflow_metadata")

	run := func(t *testing.T, cache *calls.Cache) []string {
		t.Helper()
		r := NewRuleWorkflowCall("caller.yaml", cache)
		if err := r.VisitWorkflowPre(&workflowlang.Workflow{}); err != nil {
			t.Fatal(err)
		}
		j := &workflowlang.Job{
			ID:          &workflowlang.String{Value: "caller", Pos: &workflowlang.Pos{}},
			Permissions: testParseWorkflowPermissions(t, "permissions: {}"),
			WorkflowCall: &workflowlang.WorkflowCall{
				Uses:    &workflowlang.String{Value: "./permissions.yaml", Pos: &workflowlang.Pos{}},
				Inputs:  map[string]*workflowlang.WorkflowCallInput{},
				Secrets: map[string]*workflowlang.WorkflowCallSecret{},
			},
		}
		if err := r.VisitJobPre(j); err != nil {
			t.Fatal(err)
		}
		msgs := []string{}
		for _, err := range r.Errs() {
			msgs = append(msgs, err.Message)
		}
		return msgs
	}

	fromFile := run(t, calls.NewCache(root, root, nil))

	src, err := os.ReadFile(filepath.Join(root, "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	w, errs := Parse(src)
	if w == nil {
		t.Fatal("callee was not parsed:", errs)
	}
	var event *workflowlang.WorkflowCallEvent
	for _, e := range w.On {
		if ev, ok := e.(*workflowlang.WorkflowCallEvent); ok {
			event = ev
		}
	}
	if event == nil {
		t.Fatal("callee has no workflow_call event")
	}
	astCache := calls.NewCache(root, root, nil)
	astCache.WriteWorkflowCallEventFromWorkflow("permissions.yaml", event, w)
	fromNode := run(t, astCache)

	if len(fromFile) == 0 {
		t.Fatal("no diagnostic was reported")
	}
	if diff := cmp.Diff(fromFile, fromNode); diff != "" {
		t.Fatal("diagnostics differ between cache population paths. diff:\n" + diff)
	}
	for _, id := range []string{"aliased-perms", "base", "duplicate"} {
		if !slices.ContainsFunc(fromFile, func(m string) bool {
			return strings.Contains(m, fmt.Sprintf("nested job %q", id))
		}) {
			t.Errorf("no diagnostic mentions job %q in %v", id, fromFile)
		}
	}
}

func TestRuleWorkflowCallCheckPermissions(t *testing.T) {
	scopes := make([]string, 0, len(allPermissionScopes))
	writeAll := calls.PermissionScopeLevels{}
	for s, vs := range allPermissionScopes {
		scopes = append(scopes, s)
		if slices.Contains(vs, "write") {
			writeAll[s] = calls.PermissionLevelWrite
		} else {
			writeAll[s] = calls.PermissionLevelRead
		}
	}
	slices.Sort(scopes)

	writeAllWant, writeAllHave := []string{}, []string{}
	for _, s := range scopes {
		if s == "contents" {
			continue
		}
		writeAllWant = append(writeAllWant, s+": "+writeAll[s].String())
		writeAllHave = append(writeAllHave, s+": none")
	}
	writeAllMsg := fmt.Sprintf(
		"nested job \"j\" of \"./callee.yaml\" requires %s but the calling job grants %s",
		stringutil.Quotes(writeAllWant),
		stringutil.Quotes(writeAllHave),
	)

	tests := []struct {
		what          string
		jobPerms      string
		workflowPerms string
		jobIf         bool
		callee        map[string]calls.PermissionScopeLevels
		cfg           *Config
		errs          []string
	}{
		{
			what:     "job grant covers the requirement",
			jobPerms: "permissions:\n  pull-requests: write",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
		},
		{
			what:     "job grants read but write is required",
			jobPerms: "permissions:\n  pull-requests: read",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: read\"",
			},
		},
		{
			what:     "job grants write where read is required",
			jobPerms: "permissions:\n  contents: write",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"contents": calls.PermissionLevelRead}},
		},
		{
			what:     "incorrectly cased scope does not grant permission",
			jobPerms: "permissions:\n  Contents: write",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"contents": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"contents: write\" but the calling job grants \"contents: none\"",
			},
		},
		{
			what:          "job says nothing so the workflow block is used",
			workflowPerms: "permissions:\n  pull-requests: write",
			callee:        map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
		},
		{
			what:          "job block replaces the workflow block",
			jobPerms:      "permissions:\n  contents: read",
			workflowPerms: "permissions:\n  pull-requests: write",
			callee:        map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:   "restricted default covers contents",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"contents": calls.PermissionLevelRead}},
		},
		{
			what:   "restricted default does not cover pull-requests",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:   "permissive default covers pull-requests",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionPermissive},
		},
		{
			what:   "permissive default never covers id-token",
			callee: map[string]calls.PermissionScopeLevels{"attest": {"id-token": calls.PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionPermissive},
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:   "restricted assumption set explicitly",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionRestricted},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:     "empty caller block grants nothing",
			jobPerms: "permissions: {}",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"contents": calls.PermissionLevelRead}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"contents: read\" but the calling job grants \"contents: none\"",
			},
		},
		{
			what:     "caller read-all does not cover id-token",
			jobPerms: "permissions: read-all",
			callee:   map[string]calls.PermissionScopeLevels{"attest": {"id-token": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:     "caller write-all covers everything write is available for",
			jobPerms: "permissions: write-all",
			callee: map[string]calls.PermissionScopeLevels{
				"snapshot": {
					"contents":      calls.PermissionLevelWrite,
					"id-token":      calls.PermissionLevelWrite,
					"pull-requests": calls.PermissionLevelWrite,
				},
			},
		},
		{
			what:     "one error lists every missing scope of a job",
			jobPerms: "permissions:\n  contents: write",
			callee: map[string]calls.PermissionScopeLevels{
				"snapshot": {
					"actions":  calls.PermissionLevelWrite,
					"contents": calls.PermissionLevelWrite,
					"issues":   calls.PermissionLevelWrite,
				},
			},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"actions: write\", \"issues: write\" but the calling job grants \"actions: none\", \"issues: none\"",
			},
		},
		{
			what:     "callee requiring every scope produces a single error",
			jobPerms: "permissions:\n  contents: write",
			callee:   map[string]calls.PermissionScopeLevels{"j": writeAll},
			errs:     []string{writeAllMsg},
		},
		{
			what:   "callee declares no permissions",
			callee: map[string]calls.PermissionScopeLevels{},
		},
		{
			what:   "callee job requires nothing",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {}},
		},
		{
			what:     "invalid caller block is not reported by this rule",
			jobPerms: "permissions: bogus",
			callee:   map[string]calls.PermissionScopeLevels{"snapshot": {"contents": calls.PermissionLevelRead}},
		},
		{
			what:   "condition on the calling job does not suppress the check",
			jobIf:  true,
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:     "errors are sorted by job ID",
			jobPerms: "permissions: {}",
			callee: map[string]calls.PermissionScopeLevels{
				"zulu":  {"contents": calls.PermissionLevelRead},
				"alpha": {"issues": calls.PermissionLevelWrite},
			},
			errs: []string{
				"nested job \"alpha\" of \"./callee.yaml\" requires \"issues: write\" but the calling job grants \"issues: none\"",
				"nested job \"zulu\" of \"./callee.yaml\" requires \"contents: read\" but the calling job grants \"contents: none\"",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			cwd := filepath.Join("path", "to", "project")
			cache := calls.NewCache(cwd, cwd, nil)
			cache.Store("./callee.yaml", &calls.Metadata{JobPermissions: tc.callee}, nil)

			r := NewRuleWorkflowCall("caller.yaml", cache)
			if tc.cfg != nil {
				r.SetConfig(tc.cfg)
			}

			w := &workflowlang.Workflow{Permissions: testParseWorkflowPermissions(t, tc.workflowPerms)}
			if err := r.VisitWorkflowPre(w); err != nil {
				t.Fatal(err)
			}

			j := &workflowlang.Job{
				ID:          &workflowlang.String{Value: "caller", Pos: &workflowlang.Pos{}},
				Permissions: testParseWorkflowPermissions(t, tc.jobPerms),
				WorkflowCall: &workflowlang.WorkflowCall{
					Uses:    &workflowlang.String{Value: "./callee.yaml", Pos: &workflowlang.Pos{}},
					Inputs:  map[string]*workflowlang.WorkflowCallInput{},
					Secrets: map[string]*workflowlang.WorkflowCallSecret{},
				},
			}
			if tc.jobIf {
				j.If = &workflowlang.String{Value: "${{ github.event_name == 'push' }}", Pos: &workflowlang.Pos{}}
			}
			if err := r.VisitJobPre(j); err != nil {
				t.Fatal(err)
			}

			have := []string{}
			for _, err := range r.Errs() {
				have = append(have, err.Message)
			}
			want := tc.errs
			if want == nil {
				want = []string{}
			}
			if diff := cmp.Diff(want, have); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

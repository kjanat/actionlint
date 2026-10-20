package builtin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/builtin"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/semantics/permission"
	"actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/calls"

	"github.com/google/go-cmp/cmp"
)

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
	w, errs := workflowlang.ParseDocument(b)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}
	return w.Permissions
}

func TestRuleWorkflowCallPermissionsCachePathParity(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "reusable_workflow_metadata")

	run := func(t *testing.T, cache *calls.Cache) []string {
		t.Helper()
		r := builtin.NewRuleWorkflowCall("caller.yaml", cache)
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
	w, errs := workflowlang.ParseDocument(src)
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
	scopes := make([]string, 0, len(permission.Scopes()))
	writeAll := calls.PermissionScopeLevels{}
	for s, vs := range permission.Scopes() {
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
		cfg           *repoconfig.Config
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
			cfg:    &repoconfig.Config{AssumeDefaultPermissions: repoconfig.DefaultPermissionsAssumptionPermissive},
		},
		{
			what:   "permissive default never covers id-token",
			callee: map[string]calls.PermissionScopeLevels{"attest": {"id-token": calls.PermissionLevelWrite}},
			cfg:    &repoconfig.Config{AssumeDefaultPermissions: repoconfig.DefaultPermissionsAssumptionPermissive},
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:   "restricted assumption set explicitly",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			cfg:    &repoconfig.Config{AssumeDefaultPermissions: repoconfig.DefaultPermissionsAssumptionRestricted},
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

			r := builtin.NewRuleWorkflowCall("caller.yaml", cache)
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

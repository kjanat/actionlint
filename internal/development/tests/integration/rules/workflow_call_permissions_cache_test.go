package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/repository"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/workflows/permissions"
	"actionlint.kjanat.dev/v2/internal/results/messages"
	"github.com/google/go-cmp/cmp"
)

func testParseWorkflowPermissions(t *testing.T, src string) *definition.Permissions {
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
	w, errs := parser.ParseWorkflow(b)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}
	return w.Permissions
}

func TestRuleWorkflowCallPermissionsCachePathParity(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "..", "input", "references", "workflows", "testdata", "reusable-workflows")

	run := func(t *testing.T, cache *workflows.Cache) []string {
		t.Helper()
		if err := cache.LoadCalls(t.Context(), []string{"./permissions.yaml"}); err != nil {
			t.Fatal(err)
		}
		r := workflowCallVisitor(t, declarations.Workflow(&definition.Workflow{}), cache, repository.Snapshot{})
		if err := r.VisitWorkflowPre(declarations.Workflow(&definition.Workflow{})); err != nil {
			t.Fatal(err)
		}
		j := &definition.Job{
			ID:          &definition.String{Value: "caller", Pos: &source.Position{}},
			Permissions: testParseWorkflowPermissions(t, "permissions: {}"),
			WorkflowCall: &definition.WorkflowCall{
				Uses:    &definition.String{Value: "./permissions.yaml", Pos: &source.Position{}},
				Inputs:  map[string]*definition.WorkflowCallInput{},
				Secrets: map[string]*definition.WorkflowCallSecret{},
			},
		}
		if err := r.VisitJobPre(declarations.Job(j)); err != nil {
			t.Fatal(err)
		}
		msgs := []string{}
		for _, err := range r.Errs() {
			msgs = append(msgs, err.Message)
		}
		return msgs
	}

	fromFile := run(t, newCapturedWorkflowCache(t, root, root, nil))

	src, err := os.ReadFile(filepath.Join(root, "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	w, errs := parser.ParseWorkflow(src)
	if w == nil {
		t.Fatal("callee was not parsed:", errs)
	}
	var event *definition.WorkflowCallEvent
	for _, e := range w.On {
		if ev, ok := e.(*definition.WorkflowCallEvent); ok {
			event = ev
		}
	}
	if event == nil {
		t.Fatal("callee has no workflow_call event")
	}
	astCache, snapshots := newCapturedWorkflowCacheAndSnapshots(t, root, root, nil)
	path := filepath.Join(root, "permissions.yaml")
	snapshot, err := snapshots.Document(path, src, parsing.Workflow(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := astCache.Seed(snapshot); err != nil {
		t.Fatal(err)
	}
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
	t.Parallel()
	scopes := make([]string, 0, len(permissions.Scopes()))
	writeAll := calls.PermissionScopeLevels{}
	for s, vs := range permissions.Scopes() {
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
		messages.Quotes(writeAllWant),
		messages.Quotes(writeAllHave),
	)

	tests := []struct {
		what          string
		jobPerms      string
		workflowPerms string
		jobIf         bool
		callee        map[string]calls.PermissionScopeLevels
		cfg           *configuration.Config
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
			what:   "unknown default does not invent a grant",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
		},
		{
			what:   "permissive default covers pull-requests",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			cfg:    permissionsConfig(t, "write"),
		},
		{
			what:   "permissive default never covers id-token",
			callee: map[string]calls.PermissionScopeLevels{"attest": {"id-token": calls.PermissionLevelWrite}},
			cfg:    permissionsConfig(t, "write"),
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:   "restricted assumption set explicitly",
			callee: map[string]calls.PermissionScopeLevels{"snapshot": {"pull-requests": calls.PermissionLevelWrite}},
			cfg:    permissionsConfig(t, "read"),
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
			cfg:    permissionsConfig(t, "read"),
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
			t.Parallel()
			cwd := filepath.Join("path", "to", "project")
			cache := newCapturedWorkflowCache(t, cwd, cwd, nil)
			cache.Store("./callee.yaml", &calls.Metadata{JobPermissions: tc.callee})

			w := &definition.Workflow{Permissions: testParseWorkflowPermissions(t, tc.workflowPerms)}
			r := workflowCallVisitor(t, declarations.Workflow(w), cache, configuration.RepositoryFacts(tc.cfg, repository.Snapshot{}))
			if err := r.VisitWorkflowPre(declarations.Workflow(w)); err != nil {
				t.Fatal(err)
			}

			j := &definition.Job{
				ID:          &definition.String{Value: "caller", Pos: &source.Position{}},
				Permissions: testParseWorkflowPermissions(t, tc.jobPerms),
				WorkflowCall: &definition.WorkflowCall{
					Uses:    &definition.String{Value: "./callee.yaml", Pos: &source.Position{}},
					Inputs:  map[string]*definition.WorkflowCallInput{},
					Secrets: map[string]*definition.WorkflowCallSecret{},
				},
			}
			if tc.jobIf {
				j.If = &definition.String{Value: "${{ github.event_name == 'push' }}", Pos: &source.Position{}}
			}
			if err := r.VisitJobPre(declarations.Job(j)); err != nil {
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

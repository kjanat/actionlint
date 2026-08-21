package actionlint

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

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
			c := NewLocalReusableWorkflowCache(nil, "", nil)
			r := NewRuleWorkflowCall("", c)
			j := &Job{
				WorkflowCall: &WorkflowCall{
					Uses: &String{
						Value: tc.uses,
						Pos:   &Pos{},
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
	w := &Workflow{
		On: []Event{
			&WorkflowCallEvent{
				Pos: &Pos{},
			},
		},
	}

	j := &Job{
		WorkflowCall: &WorkflowCall{
			Uses: &String{
				Value: "o/r/w.yaml@r",
				Pos:   &Pos{},
			},
		},
	}

	c := NewLocalReusableWorkflowCache(nil, "", nil)
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
	s := func(v string) *String {
		return &String{Value: v, Pos: &Pos{}}
	}
	w := &Workflow{
		On: []Event{
			&WorkflowCallEvent{
				Inputs: []*WorkflowCallEventInput{
					{
						Name: s("input1"),
						Type: WorkflowCallEventInputTypeString,
						ID:   "input1",
					},
				},
				Outputs: map[string]*WorkflowCallEventOutput{
					"output1": {Name: s("output1")},
				},
				Secrets: map[string]*WorkflowCallEventSecret{
					"secret1": {Name: s("secret1")},
				},
				Pos: &Pos{},
			},
		},
	}

	cwd := filepath.Join("path", "to", "project")
	c := NewLocalReusableWorkflowCache(&Project{cwd, nil}, cwd, nil)
	r := NewRuleWorkflowCall("test-workflow.yaml", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	errs := r.Errs()
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	m, ok := c.readCache("./test-workflow.yaml")
	if !ok {
		t.Fatal("no metadata was created")
	}

	want := &ReusableWorkflowMetadata{
		Inputs: ReusableWorkflowMetadataInputs{
			"input1": {"input1", false, StringType{}},
		},
		Outputs: ReusableWorkflowMetadataOutputs{
			"output1": {"output1"},
		},
		Secrets: ReusableWorkflowMetadataSecrets{
			"secret1": {"secret1", false},
		},
	}

	if diff := cmp.Diff(want, m); diff != "" {
		t.Fatal(diff)
	}
}

func TestRuleWorkflowCallCheckReusableWorkflowCall(t *testing.T) {
	cwd := filepath.Join("testdata", "reusable_workflow_metadata")
	cache := NewLocalReusableWorkflowCache(&Project{cwd, nil}, cwd, nil)

	for i, md := range []*ReusableWorkflowMetadata{
		// workflow0.yaml
		{
			Inputs: ReusableWorkflowMetadataInputs{
				"optional_input": {"optional_input", false, StringType{}},
				"required_input": {"required_input", true, StringType{}},
			},
			Outputs: ReusableWorkflowMetadataOutputs{
				"output": {"output"},
			},
			Secrets: ReusableWorkflowMetadataSecrets{
				"optional_secret": {"optional_secret", false},
				"required_secret": {"required_secret", true},
			},
		},
		// workflow1.yaml: Inputs and outputs in upper case (#216)
		{
			Inputs: ReusableWorkflowMetadataInputs{
				"optional_input": {"OPTIONAL_INPUT", false, StringType{}},
				"required_input": {"REQUIRED_INPUT", true, StringType{}},
			},
			Outputs: ReusableWorkflowMetadataOutputs{
				"output": {"OUTPUT"},
			},
			Secrets: ReusableWorkflowMetadataSecrets{
				"optional_secret": {"OPTIONAL_SECRET", false},
				"required_secret": {"REQUIRED_SECRET", true},
			},
		},
		// workflow2.yaml: No input and secret are defined
		{
			Inputs:  ReusableWorkflowMetadataInputs{},
			Outputs: ReusableWorkflowMetadataOutputs{},
			Secrets: ReusableWorkflowMetadataSecrets{},
		},
	} {
		cache.writeCache(fmt.Sprintf("./workflow%d.yaml", i), md)
	}

	tests := []struct {
		what           string
		uses           string
		inputs         []string
		secrets        []string
		inheritSecrets bool
		errs           []string
	}{
		{
			what:    "all",
			uses:    "./workflow0.yaml",
			inputs:  []string{"optional_input", "required_input"},
			secrets: []string{"optional_secret", "required_secret"},
		},
		{
			what:    "only required",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input"},
			secrets: []string{"required_secret"},
		},
		{
			what:    "unknown workflow",
			uses:    "./unknown-workflow.yaml",
			inputs:  []string{"aaa", "bbb"},
			secrets: []string{"xxx", "yyy"},
			errs: []string{
				"could not read reusable workflow file for \"./unknown-workflow.yaml\":",
			},
		},
		{
			what:    "missing required input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"optional_input"},
			secrets: []string{"optional_secret"},
			errs: []string{
				"input \"required_input\" is required",
				"secret \"required_secret\" is required",
			},
		},
		{
			what:    "undefined input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input", "unknown_input"},
			secrets: []string{"required_secret", "unknown_secret"},
			errs: []string{
				"input \"unknown_input\" is not defined in \"./workflow0.yaml\" reusable workflow. defined inputs are \"optional_input\", \"required_input\"",
				"secret \"unknown_secret\" is not defined in \"./workflow0.yaml\" reusable workflow. defined secrets are \"optional_secret\", \"required_secret\"",
			},
		},
		{
			what:           "inherit secrets",
			uses:           "./workflow0.yaml",
			inputs:         []string{"required_input"},
			secrets:        []string{"unknown_secret", "optional_secret"},
			inheritSecrets: true,
		},
		{
			what:    "read workflow",
			uses:    "./ok.yaml", // Defined in testdata/reusable_workflow_metadata/ok.yaml
			inputs:  []string{"input2"},
			secrets: []string{"secret2"},
		},
		{
			what: "read broken workflow",
			uses: "./broken.yaml", // Defined in testdata/reusable_workflow_metadata/broken.yaml
			errs: []string{
				"error while parsing reusable workflow \"./broken.yaml\"",
			},
		},
		{
			what: "external workflow call with no input and no secret",
			uses: "owner/repo/path/to/workflow@main",
		},
		{
			what:    "external workflow call with inputs and secrets",
			uses:    "owner/repo/path/to/workflow@main",
			inputs:  []string{"aaa", "bbb"},
			secrets: []string{"xxx", "yyy"},
		},
		{
			what:    "call in upper case and workflow in lower case",
			uses:    "./workflow0.yaml",
			inputs:  []string{"OPTIONAL_INPUT", "REQUIRED_INPUT"},
			secrets: []string{"OPTIONAL_SECRET", "REQUIRED_SECRET"},
		},
		{
			what:    "call in lower case and workflow in upper case",
			uses:    "./workflow1.yaml",
			inputs:  []string{"optional_input", "required_input"},
			secrets: []string{"optional_secret", "required_secret"},
		},
		{
			what:    "call in upper case and workflow in upper case",
			uses:    "./workflow1.yaml",
			inputs:  []string{"OPTIONAL_INPUT", "REQUIRED_INPUT"},
			secrets: []string{"OPTIONAL_SECRET", "REQUIRED_SECRET"},
		},
		{
			what:    "undefined upper input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input", "UNKNOWN_INPUT"},
			secrets: []string{"required_secret", "UNKNOWN_SECRET"},
			errs: []string{
				"input \"UNKNOWN_INPUT\" is not defined in \"./workflow0.yaml\"",
				"secret \"UNKNOWN_SECRET\" is not defined in \"./workflow0.yaml\"",
			},
		},
		{
			what:    "no input and secret defined",
			uses:    "./workflow2.yaml",
			inputs:  []string{"unknown_input"},
			secrets: []string{"unknown_secret"},
			errs: []string{
				"input \"unknown_input\" is not defined in \"./workflow2.yaml\" reusable workflow. no input is defined",
				"secret \"unknown_secret\" is not defined in \"./workflow2.yaml\" reusable workflow. no secret is defined",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			r := NewRuleWorkflowCall("this-workflow.yaml", cache)

			w := &Workflow{
				On: []Event{
					&WorkflowCallEvent{
						Pos: &Pos{},
					},
				},
			}
			if err := r.VisitWorkflowPre(w); err != nil {
				t.Fatal(err)
			}

			c := &WorkflowCall{
				Uses:           &String{Value: tc.uses, Pos: &Pos{}},
				Inputs:         map[string]*WorkflowCallInput{},
				Secrets:        map[string]*WorkflowCallSecret{},
				InheritSecrets: tc.inheritSecrets,
			}
			for _, i := range tc.inputs {
				c.Inputs[strings.ToLower(i)] = &WorkflowCallInput{
					Name:  &String{Value: i, Pos: &Pos{}},
					Value: &String{Value: "", Pos: &Pos{}},
				}
			}
			for _, s := range tc.secrets {
				c.Secrets[strings.ToLower(s)] = &WorkflowCallSecret{
					Name:  &String{Value: s, Pos: &Pos{}},
					Value: &String{Value: "", Pos: &Pos{}},
				}
			}

			j := &Job{WorkflowCall: c}
			if err := r.VisitJobPre(j); err != nil {
				t.Fatal(err)
			}

			errs := []string{}
			for _, err := range r.Errs() {
				errs = append(errs, err.Error())
			}
			sort.Strings(errs)

			if len(errs) != len(tc.errs) {
				t.Fatalf(
					"Number of errors is unexpected. %d errors was expected but got %d errors. Expected errors are %v but actual errors are %v",
					len(tc.errs),
					len(errs),
					tc.errs,
					errs,
				)
			}

			for i, have := range errs {
				want := tc.errs[i]
				if !strings.Contains(have, want) {
					t.Errorf("%d-th error is unexpected. %q should be contained in error message %q", i, want, have)
				}
			}
		})
	}
}

func testParseWorkflowPermissions(t *testing.T, src string) *Permissions {
	t.Helper()
	if src == "" {
		return nil
	}
	b := []byte("on: push\n" + src + "\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n")
	w, errs := Parse(b)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}
	return w.Permissions
}

func TestRuleWorkflowCallCheckPermissions(t *testing.T) {
	scopes := make([]string, 0, len(allPermissionScopes))
	writeAll := PermissionScopeLevels{}
	for s, vs := range allPermissionScopes {
		scopes = append(scopes, s)
		if slices.Contains(vs, "write") {
			writeAll[s] = PermissionLevelWrite
		} else {
			writeAll[s] = PermissionLevelRead
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
		quotes(writeAllWant),
		quotes(writeAllHave),
	)

	tests := []struct {
		what          string
		jobPerms      string
		workflowPerms string
		jobIf         bool
		callee        map[string]PermissionScopeLevels
		cfg           *Config
		errs          []string
	}{
		{
			what:     "job grant covers the requirement",
			jobPerms: "permissions:\n  pull-requests: write",
			callee:   map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
		},
		{
			what:     "job grants read but write is required",
			jobPerms: "permissions:\n  pull-requests: read",
			callee:   map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: read\"",
			},
		},
		{
			what:     "job grants write where read is required",
			jobPerms: "permissions:\n  contents: write",
			callee:   map[string]PermissionScopeLevels{"snapshot": {"contents": PermissionLevelRead}},
		},
		{
			what:          "job says nothing so the workflow block is used",
			workflowPerms: "permissions:\n  pull-requests: write",
			callee:        map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
		},
		{
			what:          "job block replaces the workflow block",
			jobPerms:      "permissions:\n  contents: read",
			workflowPerms: "permissions:\n  pull-requests: write",
			callee:        map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:   "restricted default covers contents",
			callee: map[string]PermissionScopeLevels{"snapshot": {"contents": PermissionLevelRead}},
		},
		{
			what:   "restricted default does not cover pull-requests",
			callee: map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:   "permissive default covers pull-requests",
			callee: map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionPermissive},
		},
		{
			what:   "permissive default never covers id-token",
			callee: map[string]PermissionScopeLevels{"attest": {"id-token": PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionPermissive},
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:   "restricted assumption set explicitly",
			callee: map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			cfg:    &Config{AssumeDefaultPermissions: DefaultPermissionsAssumptionRestricted},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:     "empty caller block grants nothing",
			jobPerms: "permissions: {}",
			callee:   map[string]PermissionScopeLevels{"snapshot": {"contents": PermissionLevelRead}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"contents: read\" but the calling job grants \"contents: none\"",
			},
		},
		{
			what:     "caller read-all does not cover id-token",
			jobPerms: "permissions: read-all",
			callee:   map[string]PermissionScopeLevels{"attest": {"id-token": PermissionLevelWrite}},
			errs: []string{
				"nested job \"attest\" of \"./callee.yaml\" requires \"id-token: write\" but the calling job grants \"id-token: none\"",
			},
		},
		{
			what:     "caller write-all covers everything write is available for",
			jobPerms: "permissions: write-all",
			callee: map[string]PermissionScopeLevels{
				"snapshot": {
					"contents":      PermissionLevelWrite,
					"id-token":      PermissionLevelWrite,
					"pull-requests": PermissionLevelWrite,
				},
			},
		},
		{
			what:     "one error lists every missing scope of a job",
			jobPerms: "permissions:\n  contents: write",
			callee: map[string]PermissionScopeLevels{
				"snapshot": {
					"actions":  PermissionLevelWrite,
					"contents": PermissionLevelWrite,
					"issues":   PermissionLevelWrite,
				},
			},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"actions: write\", \"issues: write\" but the calling job grants \"actions: none\", \"issues: none\"",
			},
		},
		{
			what:     "callee requiring every scope produces a single error",
			jobPerms: "permissions:\n  contents: write",
			callee:   map[string]PermissionScopeLevels{"j": writeAll},
			errs:     []string{writeAllMsg},
		},
		{
			what:   "callee declares no permissions",
			callee: map[string]PermissionScopeLevels{},
		},
		{
			what:   "callee job requires nothing",
			callee: map[string]PermissionScopeLevels{"snapshot": {}},
		},
		{
			what:     "invalid caller block is not reported by this rule",
			jobPerms: "permissions: bogus",
			callee:   map[string]PermissionScopeLevels{"snapshot": {"contents": PermissionLevelRead}},
		},
		{
			what:   "condition on the calling job does not suppress the check",
			jobIf:  true,
			callee: map[string]PermissionScopeLevels{"snapshot": {"pull-requests": PermissionLevelWrite}},
			errs: []string{
				"nested job \"snapshot\" of \"./callee.yaml\" requires \"pull-requests: write\" but the calling job grants \"pull-requests: none\"",
			},
		},
		{
			what:     "errors are sorted by job ID",
			jobPerms: "permissions: {}",
			callee: map[string]PermissionScopeLevels{
				"zulu":  {"contents": PermissionLevelRead},
				"alpha": {"issues": PermissionLevelWrite},
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
			cache := NewLocalReusableWorkflowCache(&Project{cwd, nil}, cwd, nil)
			cache.writeCache("./callee.yaml", &ReusableWorkflowMetadata{JobPermissions: tc.callee})

			r := NewRuleWorkflowCall("caller.yaml", cache)
			if tc.cfg != nil {
				r.SetConfig(tc.cfg)
			}

			w := &Workflow{Permissions: testParseWorkflowPermissions(t, tc.workflowPerms)}
			if err := r.VisitWorkflowPre(w); err != nil {
				t.Fatal(err)
			}

			j := &Job{
				ID:          &String{Value: "caller", Pos: &Pos{}},
				Permissions: testParseWorkflowPermissions(t, tc.jobPerms),
				WorkflowCall: &WorkflowCall{
					Uses:    &String{Value: "./callee.yaml", Pos: &Pos{}},
					Inputs:  map[string]*WorkflowCallInput{},
					Secrets: map[string]*WorkflowCallSecret{},
				},
			}
			if tc.jobIf {
				j.If = &String{Value: "${{ github.event_name == 'push' }}", Pos: &Pos{}}
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

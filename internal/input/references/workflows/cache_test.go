package workflows

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	permission "actionlint.kjanat.dev/v2/internal/model/workflows/permissions"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"github.com/google/go-cmp/cmp"
)

var testReusableWorkflowWantedMetadata *calls.Metadata = &calls.Metadata{
	Inputs: calls.Inputs{
		"input1": {Name: "input1", Required: false, Type: calls.InputTypeString},
		"input2": {Name: "input2", Required: true, Type: calls.InputTypeBoolean},
	},
	Outputs: calls.Outputs{
		"output1": {Name: "output1"},
	},
	Secrets: calls.Secrets{
		"secret1": {Name: "secret1", Required: false},
		"secret2": {Name: "secret2", Required: true},
	},
}

func TestReusableWorkflowCacheFindMetadataOK(t *testing.T) {
	t.Parallel()
	proj := filepath.Join("testdata", "reusable-workflows")
	c := newCapturedWorkflowCache(t, proj, "", nil)

	m, err := c.Load(t.Context(), "./ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(m, testReusableWorkflowWantedMetadata); diff != "" {
		t.Fatal(diff)
	}

	m2, err := c.Load(t.Context(), "./ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, cached := c.cache["./ok.yaml"]; !cached {
		t.Error("metadata is not cached")
	}
	if diff := cmp.Diff(m, m2); diff != "" {
		t.Error(diff)
	}
	if m == m2 {
		t.Error("lookups share one metadata record")
	}
}

func TestReusableWorkflowCacheFindMetadataError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what string
		spec string
		want string
	}{
		{
			what: "broken workflow",
			spec: "./broken.yaml",
			want: "error while parsing reusable workflow \"./broken.yaml\"",
		},
		{
			what: "no hook",
			spec: "./no_hook.yaml",
			want: "\"workflow_call\" event trigger is not found in \"on:\"",
		},
		{
			what: "no on",
			spec: "./no_on.yaml",
			want: "\"on:\" is not found",
		},
		{
			what: "not existing workflow",
			spec: "./this-workflow-does-not-exist.yaml",
			want: "could not read reusable workflow file for \"./this-workflow-does-not-exist.yaml\":",
		},
		{
			what: "broken inputs",
			spec: "./broken_inputs.yaml",
			want: "error while parsing reusable workflow \"./broken_inputs.yaml\"",
		},
		{
			what: "broken secrets",
			spec: "./broken_secrets.yaml",
			want: "error while parsing reusable workflow \"./broken_secrets.yaml\"",
		},
		{
			what: "recursive alias",
			spec: "./recursive_alias.yaml",
			want: "recursive alias \"job\" is found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			proj := filepath.Join("testdata", "reusable-workflows")
			c := newCapturedWorkflowCache(t, proj, "", nil)
			_, err := c.Load(t.Context(), tc.spec)
			if err == nil {
				t.Fatal("no error happened")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("unexpected error. wanted %q but got %q", tc.want, msg)
			}
			// Each caller receives the cached failure at its own lookup.
			m, err := c.Load(t.Context(), tc.spec)
			if err == nil || err.Error() != msg {
				t.Fatal("cached failure changed:", err)
			}
			if m != nil {
				t.Fatal("nil is not cached:", m)
			}
		})
	}
}

func TestReusableWorkflowCacheFindMetadataSkipParsing(t *testing.T) {
	t.Parallel()
	p := filepath.Join("testdata", "reusable-workflows")
	tests := []struct {
		what string
		proj string
		spec string
	}{
		{
			what: "no project",
			proj: "",
			spec: "./ok.yaml",
		},
		{
			what: "external workflow",
			proj: p,
			spec: "repo/owner/workflow@main",
		},
		{
			what: "template placeholder",
			proj: p,
			spec: "./${{ some_expression }}.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c := newCapturedWorkflowCache(t, tc.proj, "", nil)
			m, err := c.Load(t.Context(), tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if m != nil {
				t.Fatal("metadata should be nil:", m)
			}
			m, err = c.Load(t.Context(), tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if m != nil {
				t.Fatal("nil is not cached:", m)
			}
		})
	}
}

func TestReusableWorkflowConvertWorkflowPathToSpec(t *testing.T) {
	t.Parallel()
	p := filepath.Join("path", "to", "project")
	cwd := filepath.Join("path", "to", "project", "cwd")
	tests := []struct {
		what string
		proj string
		path string
		want string
		ok   bool
	}{
		{
			what: "current dir",
			proj: p,
			path: "workflow.yaml",
			want: "./cwd/workflow.yaml",
			ok:   true,
		},
		{
			what: "child dir",
			proj: p,
			path: filepath.Join("dir", "workflow.yaml"),
			want: "./cwd/dir/workflow.yaml",
			ok:   true,
		},
		{
			what: "parent dir",
			proj: p,
			path: filepath.Join("..", "dir", "workflow.yaml"),
			want: "./dir/workflow.yaml",
			ok:   true,
		},
		{
			what: "no project",
			proj: "",
			ok:   false,
		},
		{
			what: "other project",
			proj: filepath.Join("path", "to", "other-project"),
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c := newCapturedWorkflowCache(t, tc.proj, cwd, nil)
			s, ok := c.convWorkflowPathToSpec(tc.path)
			if ok != tc.ok {
				t.Fatalf("should return %v but got %v (spec=%q)", tc.ok, ok, s)
			}
			if ok && s != tc.want {
				t.Fatalf("wanted spec %q but got %q", tc.want, s)
			}
		})
	}
}

func TestReusableWorkflowMetadataFromASTNodeInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what   string
		inputs map[string]*workflowlang.WorkflowCallEventInput
		want   calls.Inputs
	}{
		{
			what: "type of inputs",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"string_input":  {Type: workflowlang.WorkflowCallEventInputTypeString},
				"number_input":  {Type: workflowlang.WorkflowCallEventInputTypeNumber},
				"bool_input":    {Type: workflowlang.WorkflowCallEventInputTypeBoolean},
				"unknown_input": {},
			},
			want: calls.Inputs{
				"string_input":  {Name: "string_input", Required: false, Type: calls.InputTypeString},
				"number_input":  {Name: "number_input", Required: false, Type: calls.InputTypeNumber},
				"bool_input":    {Name: "bool_input", Required: false, Type: calls.InputTypeBoolean},
				"unknown_input": {Name: "unknown_input", Required: false, Type: calls.InputTypeAny},
			},
		},
		{
			what: "required or optional",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"unspecified":  {},
				"not_required": {Required: &workflowlang.Bool{Value: false, Pos: &workflowlang.Pos{}}},
				"required":     {Required: &workflowlang.Bool{Value: true, Pos: &workflowlang.Pos{}}},
				"required_but_default": {
					Required: &workflowlang.Bool{Value: true, Pos: &workflowlang.Pos{}},
					Default:  &workflowlang.String{Pos: &workflowlang.Pos{}},
				},
				"expression": {
					Required: &workflowlang.Bool{
						Expression: &workflowlang.String{Pos: &workflowlang.Pos{}},
						Pos:        &workflowlang.Pos{},
					},
				},
			},
			want: calls.Inputs{
				"unspecified":          {Name: "unspecified", Required: false, Type: calls.InputTypeAny},
				"not_required":         {Name: "not_required", Required: false, Type: calls.InputTypeAny},
				"required":             {Name: "required", Required: true, Type: calls.InputTypeAny},
				"required_but_default": {Name: "required_but_default", Required: false, Type: calls.InputTypeAny},
				"expression":           {Name: "expression", Required: false, Type: calls.InputTypeAny},
			},
		},
		{
			what:   "empty",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{},
			want:   calls.Inputs{},
		},
		{
			what: "upper case input",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"MY_INPUT": {Type: workflowlang.WorkflowCallEventInputTypeString},
			},
			want: calls.Inputs{
				"my_input": {Name: "MY_INPUT", Required: false, Type: calls.InputTypeString},
			},
		},
		{
			what: "upper case inputs",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"MY_INPUT": {},
			},
			want: calls.Inputs{
				"my_input": {Name: "MY_INPUT", Required: false, Type: calls.InputTypeAny},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			cwd := filepath.Join("path", "to", "project")
			proj := cwd
			c := newCapturedWorkflowCache(t, proj, cwd, nil)
			e := &workflowlang.WorkflowCallEvent{Inputs: []*workflowlang.WorkflowCallEventInput{}}
			for n, i := range tc.inputs {
				i.Name = &workflowlang.String{Value: n, Pos: &workflowlang.Pos{}}
				i.ID = strings.ToLower(n)
				e.Inputs = append(e.Inputs, i)
			}

			c.WriteWorkflowCallEvent(filepath.Join("foo", "test.yaml"), e)

			m, _, ok := c.Lookup("./foo/test.yaml")
			if !ok {
				t.Fatal("Event was not converted to event")
			}

			if diff := cmp.Diff(m.Inputs, tc.want); diff != "" {
				t.Fatal(diff)
			}
			if len(m.Outputs) != 0 {
				t.Error("Outputs are not empty", m.Outputs)
			}
			if len(m.Secrets) != 0 {
				t.Error("Secrets are not empty", m.Secrets)
			}
		})
	}
}

func TestReusableWorkflowMetadataFromASTNodeOutputs(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{},
		{"foo"},
		{"a", "b", "c"},
		{"A", "B", "C"},
	}
	for _, outputs := range tests {
		t.Run(fmt.Sprintf("%s", outputs), func(t *testing.T) {
			t.Parallel()
			cwd := filepath.Join("path", "to", "project")
			proj := cwd
			c := newCapturedWorkflowCache(t, proj, cwd, nil)
			e := &workflowlang.WorkflowCallEvent{Outputs: map[string]*workflowlang.WorkflowCallEventOutput{}}
			for _, o := range outputs {
				e.Outputs[strings.ToLower(o)] = &workflowlang.WorkflowCallEventOutput{
					Name: &workflowlang.String{Value: o, Pos: &workflowlang.Pos{}},
				}
			}

			c.WriteWorkflowCallEvent(filepath.Join("foo", "test.yaml"), e)

			m, _, ok := c.Lookup("./foo/test.yaml")
			if !ok {
				t.Fatal("Event was not converted to event")
			}

			want := calls.Outputs{}
			for _, o := range outputs {
				want[strings.ToLower(o)] = calls.Output{Name: o}
			}

			if diff := cmp.Diff(m.Outputs, want); diff != "" {
				t.Fatal(diff)
			}
			if len(m.Inputs) != 0 {
				t.Error("Inputs are not empty", m.Inputs)
			}
			if len(m.Secrets) != 0 {
				t.Error("Secrets are not empty", m.Secrets)
			}
		})
	}
}

func TestReusableWorkflowMetadataFromASTNodeSecrets(t *testing.T) {
	t.Parallel()
	tests := []map[string]*workflowlang.Bool{
		{},
		{"a": nil},
		{"a": &workflowlang.Bool{Value: false, Pos: &workflowlang.Pos{}}},
		{"a": &workflowlang.Bool{Value: true, Pos: &workflowlang.Pos{}}},
		{"a": &workflowlang.Bool{Expression: &workflowlang.String{Pos: &workflowlang.Pos{}}, Pos: &workflowlang.Pos{}}},
		{
			"a": &workflowlang.Bool{Value: false, Pos: &workflowlang.Pos{}},
			"b": &workflowlang.Bool{Value: true, Pos: &workflowlang.Pos{}},
			"c": nil,
		},
		{
			"A": &workflowlang.Bool{Value: false, Pos: &workflowlang.Pos{}},
			"B": &workflowlang.Bool{Value: true, Pos: &workflowlang.Pos{}},
			"C": nil,
		},
	}
	for _, secrets := range tests {
		t.Run(fmt.Sprintf("%s", secrets), func(t *testing.T) {
			t.Parallel()
			cwd := filepath.Join("path", "to", "project")
			proj := cwd
			c := newCapturedWorkflowCache(t, proj, cwd, nil)
			e := &workflowlang.WorkflowCallEvent{Secrets: map[string]*workflowlang.WorkflowCallEventSecret{}}
			for n, r := range secrets {
				e.Secrets[strings.ToLower(n)] = &workflowlang.WorkflowCallEventSecret{
					Name:     &workflowlang.String{Value: n, Pos: &workflowlang.Pos{}},
					Required: r,
				}
			}

			c.WriteWorkflowCallEvent(filepath.Join("foo", "test.yaml"), e)

			m, _, ok := c.Lookup("./foo/test.yaml")
			if !ok {
				t.Fatal("Event was not converted to event")
			}

			want := calls.Secrets{}
			for n, r := range secrets {
				want[strings.ToLower(n)] = calls.Secret{
					Name:     n,
					Required: r != nil && r.Value,
				}
			}

			if diff := cmp.Diff(m.Secrets, want); diff != "" {
				t.Fatal(diff)
			}
			if len(m.Inputs) != 0 {
				t.Error("Inputs are not empty", m.Inputs)
			}
			if len(m.Outputs) != 0 {
				t.Error("Outputs are not empty", m.Outputs)
			}
		})
	}
}

func TestReusableWorkflowMetadataFromASTNodeDoNothing(t *testing.T) {
	t.Parallel()
	cwd := filepath.Join("path", "to", "project")
	c := newCapturedWorkflowCache(t, "", cwd, nil)
	c.WriteWorkflowCallEvent("workflow.yaml", &workflowlang.WorkflowCallEvent{})
	m, _, ok := c.Lookup("./workflow.yaml")
	if ok {
		t.Fatal("Metadata created:", m)
	}

	proj := cwd
	c = newCapturedWorkflowCache(t, proj, filepath.Join("path", "to", "another-project"), nil)
	c.WriteWorkflowCallEvent("workflow.yaml", &workflowlang.WorkflowCallEvent{})
	m, _, ok = c.Lookup("./workflow.yaml")
	if ok {
		t.Fatal("Metadata created:", m)
	}

	m1 := &calls.Metadata{}
	c = newCapturedWorkflowCache(t, proj, cwd, nil)
	c.Store("./dir/workflow.yaml", m1)
	c.WriteWorkflowCallEvent(filepath.Join("dir", "workflow.yaml"), &workflowlang.WorkflowCallEvent{})
	m2, _, ok := c.Lookup("./dir/workflow.yaml")
	if !ok {
		t.Fatal("Metadata was not created for ./dir/workflow.yaml")
	}
	if diff := cmp.Diff(m1, m2); diff != "" {
		t.Fatalf("Metadata was not cached for ./dir/workflow.yaml: %s", diff)
	}
}

func TestReusableWorkflowMetadataCacheFindOneMetadataConcurrently(t *testing.T) {
	t.Parallel()
	n := 10
	cwd := filepath.Join("testdata", "reusable-workflows")
	proj := cwd
	c := newCapturedWorkflowCache(t, proj, cwd, nil)
	ret := make(chan *calls.Metadata)
	err := make(chan error)

	for range n {
		go func() {
			m, e := c.Load(t.Context(), "./ok.yaml")
			if e != nil {
				err <- e
				return
			}
			ret <- m
		}()
	}

	ms := []*calls.Metadata{}
	errs := []error{}
	for range n {
		select {
		case m := <-ret:
			ms = append(ms, m)
		case e := <-err:
			errs = append(errs, e)
		}
	}

	if len(errs) != 0 {
		t.Fatal("Error occurred:", errs)
	}

	for _, m := range ms {
		if diff := cmp.Diff(testReusableWorkflowWantedMetadata, m); diff != "" {
			t.Fatal(diff)
		}
	}

	if len(c.cache) != 1 {
		t.Errorf("Unexpected %d caches are stored: %v", len(c.cache), c.cache)
	}
	m, _, ok := c.Lookup("./ok.yaml")
	if !ok {
		t.Fatal("Cache did not exist")
	}
	if m == nil {
		t.Fatal("nil is stored in cache")
	}
}

func TestReusableWorkflowMetadataCacheWriteFromFileAndASTNodeConcurrently(t *testing.T) {
	t.Parallel()
	n := 10
	cwd := filepath.Join("testdata", "reusable-workflows")
	proj := cwd
	c := newCapturedWorkflowCache(t, proj, cwd, nil)
	ret := make(chan struct{})
	err := make(chan error)

	fromFile := func() {
		if _, e := c.Load(t.Context(), "./ok.yaml"); e != nil {
			err <- e
			return
		}
		ret <- struct{}{}
	}
	fromNode := func() {
		c.WriteWorkflowCallEvent("workflow.yaml", &workflowlang.WorkflowCallEvent{})
		if _, _, ok := c.Lookup("./workflow.yaml"); !ok {
			err <- errors.New("Cache was not created from WorkflowCallEvent")
			return
		}
		ret <- struct{}{}
	}

	for range n / 2 {
		go fromFile()
		go fromNode()
	}

	errs := []error{}
	for range n {
		select {
		case <-ret:
		case e := <-err:
			errs = append(errs, e)
		}
	}

	if len(errs) != 0 {
		t.Fatal("Error occurred:", errs)
	}

	if len(c.cache) != 2 {
		t.Errorf("Size of cache should be 2 but got %d: %v", len(c.cache), c.cache)
	}
	if _, ok := c.cache["./ok.yaml"]; !ok {
		t.Error("Cache for ./ok.yaml was not created", c.cache)
	}
	if _, ok := c.cache["./workflow.yaml"]; !ok {
		t.Error("Cache for WorkflowCallEvent was not created", c.cache)
	}
}

func TestReusableWorkflowNullCache(t *testing.T) {
	t.Parallel()
	c := &ASTComparisonCache{Cache: NewCache("", io.Discard)}
	e := &workflowlang.WorkflowCallEvent{Inputs: []*workflowlang.WorkflowCallEventInput{}}

	// This should do nothing
	c.WriteWorkflowCallEvent(filepath.Join("foo", "test.yaml"), e)

	m, err := c.Load(t.Context(), "./foo/test.yaml")
	if m != nil {
		t.Errorf("metadata should never be found with null cache: %v", m)
	}
	if err != nil {
		t.Errorf("error should not happen since the cache simply doesn't hit: %v", err)
	}
}

func TestReusableWorkflowCacheDetachesMetadata(t *testing.T) {
	t.Parallel()
	cwd := filepath.Join("path", "to", "project")
	c := newCapturedWorkflowCache(t, cwd, cwd, nil)
	want := func() *calls.Metadata {
		return &calls.Metadata{
			Inputs:         calls.Inputs{"in": {Name: "in", Required: true, Type: calls.InputTypeString}},
			Outputs:        calls.Outputs{"out": {Name: "out"}},
			Secrets:        calls.Secrets{"token": {Name: "token", Required: true}},
			JobPermissions: map[string]calls.PermissionScopeLevels{"build": {"contents": calls.PermissionLevelRead}},
			JobCacheAccess: map[string]calls.CacheAccess{"build": {Mode: calls.DeclaredCacheMode(workflowlang.CacheModeRead), Operations: []string{"actions/cache"}}},
		}
	}
	stored := want()
	c.Store("./callee.yaml", stored)
	stored.Inputs["in"] = calls.Input{Name: "changed"}
	stored.JobPermissions["build"]["contents"] = calls.PermissionLevelWrite

	first, _, ok := c.Lookup("./callee.yaml")
	if !ok {
		t.Fatal("stored metadata was not found")
	}
	if diff := cmp.Diff(want(), first); diff != "" {
		t.Fatalf("Store kept the caller's storage: %s", diff)
	}
	delete(first.Outputs, "out")
	first.Secrets["token"] = calls.Secret{Name: "token"}
	first.JobPermissions["build"]["contents"] = calls.PermissionLevelWrite
	first.JobCacheAccess["build"].Operations[0] = "changed"

	second, _, _ := c.Lookup("./callee.yaml")
	if diff := cmp.Diff(want(), second); diff != "" {
		t.Fatalf("Lookup exposed cached storage: %s", diff)
	}
}

func TestReusableWorkflowCacheFailureNamesCallSpelling(t *testing.T) {
	t.Parallel()
	cwd := filepath.Join("testdata", "reusable-workflows")
	c := newCapturedWorkflowCache(t, cwd, cwd, nil)
	if _, err := c.LoadForCall(t.Context(), "$/broken.yaml"); err == nil || !strings.Contains(err.Error(), `error while parsing reusable workflow "$/broken.yaml"`) {
		t.Fatalf("call-site error = %v, want the call's spelling", err)
	}
	if _, err := c.Load(t.Context(), "./broken.yaml"); err == nil || !strings.Contains(err.Error(), `error while parsing reusable workflow "./broken.yaml"`) {
		t.Fatalf("cached error = %v, want the cache key", err)
	}
}

func TestReusableWorkflowMetadataJobPermissions(t *testing.T) {
	t.Parallel()
	readAll := calls.PermissionScopeLevels{}
	for s, vs := range permission.Scopes() {
		if slices.Contains(vs, "read") {
			readAll[s] = calls.PermissionLevelRead
		}
	}

	want := map[string]calls.PermissionScopeLevels{
		"inherits":      {"contents": calls.PermissionLevelRead},
		"Override":      {"pull-requests": calls.PermissionLevelWrite},
		"all-read":      readAll,
		"aliased-perms": {"contents": calls.PermissionLevelRead},
		"base":          {"id-token": calls.PermissionLevelWrite},
		"duplicate":     {"id-token": calls.PermissionLevelWrite},
	}

	proj := filepath.Join("testdata", "reusable-workflows")
	c := newCapturedWorkflowCache(t, proj, "", nil)
	m, err := c.Load(t.Context(), "./permissions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(want, m.JobPermissions); diff != "" {
		t.Fatal(diff)
	}
}

func TestReusableWorkflowMetadataJobPermissionsFromWorkflowNode(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "reusable-workflows", "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	w, errs := workflowparser.Parse(src)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}

	fromFile, err := calls.ParseMetadata(src)
	if err != nil {
		t.Fatal(err)
	}

	cwd := filepath.Join("path", "to", "project")
	c := newCapturedWorkflowCache(t, cwd, cwd, nil)
	c.WriteWorkflowCallEventFromWorkflow("test.yaml", &workflowlang.WorkflowCallEvent{}, w)
	fromNode, _, ok := c.Lookup("./test.yaml")
	if !ok {
		t.Fatal("metadata was not created")
	}

	if diff := cmp.Diff(fromFile.JobPermissions, fromNode.JobPermissions); diff != "" {
		t.Fatal(diff)
	}
}

func TestReusableWorkflowMetadataJobKeyCase(t *testing.T) {
	t.Parallel()
	for _, field := range []struct{ key, value string }{
		{"cache-mode", "write"},
		{"uses", "./leaf.yaml"},
		{"permissions", "{contents: write}"},
	} {
		for _, exact := range []bool{false, true} {
			key := field.key
			if !exact {
				key = strings.ToUpper(key)
			}
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				source := []byte(`on: workflow_call
cache-mode: read
permissions: {contents: read}
jobs:
  build:
    ` + key + ": " + field.value + "\n")
				if key != "uses" {
					source = append(source, []byte(`    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`)...)
				}
				workflow, diagnostics := workflowparser.Parse(source)
				if exact && len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				if !exact && (len(diagnostics) != 1 || diagnostics[0].Kind != "syntax-check" || !strings.Contains(diagnostics[0].Message, fmt.Sprintf("unexpected key %q", key))) {
					t.Fatalf("expected only the case-invalid key diagnostic, got %v", diagnostics)
				}
				event, ok := workflowlang.FindWorkflowCallEvent(workflow)
				if !ok {
					t.Fatal("workflow_call was not parsed")
				}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, "callee.yaml"), source, 0o600); err != nil {
					t.Fatal(err)
				}
				project := root
				astCache := newCapturedWorkflowCache(t, project, root, nil)
				astCache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				fromAST, _, ok := astCache.Lookup("./callee.yaml")
				if !ok {
					t.Fatal("AST metadata was not cached")
				}
				fileCache := newCapturedWorkflowCache(t, project, root, nil)
				for _, cache := range []*ASTComparisonCache{fileCache, astCache} {
					for pass := range 2 {
						metadata, err := cache.Load(t.Context(), "./callee.yaml")
						if err != nil {
							t.Fatal(err)
						}
						if diff := cmp.Diff(fromAST.JobCacheAccess, metadata.JobCacheAccess); diff != "" {
							t.Fatalf("lookup %d cache access differs from AST: %s", pass, diff)
						}
						if diff := cmp.Diff(fromAST.JobPermissions, metadata.JobPermissions); diff != "" {
							t.Fatalf("lookup %d permissions differ from AST: %s", pass, diff)
						}
					}
				}
			})
		}
	}
}

func TestReusableWorkflowMetadataUnusedAnchorParity(t *testing.T) {
	t.Parallel()
	root := filepath.Join("testdata", "reusable-workflows")
	src, err := os.ReadFile(filepath.Join(root, "unused_anchor.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	w, errs := workflowparser.Parse(src)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}
	if !slices.ContainsFunc(errs, func(e *workflowparser.SyntaxError) bool {
		return strings.Contains(e.Message, "defined but not used")
	}) {
		t.Fatal("parser did not report the unused anchor:", errs)
	}

	c := newCapturedWorkflowCache(t, root, root, nil)
	fromFile, err := c.Load(t.Context(), "./unused_anchor.yaml")
	if err != nil {
		t.Fatal("unused anchor made metadata extraction fail:", err)
	}
	if fromFile == nil {
		t.Fatal("no metadata was returned")
	}

	cwd := filepath.Join("path", "to", "project")
	c2 := newCapturedWorkflowCache(t, cwd, cwd, nil)
	c2.WriteWorkflowCallEventFromWorkflow("test.yaml", &workflowlang.WorkflowCallEvent{}, w)
	fromNode, _, ok := c2.Lookup("./test.yaml")
	if !ok {
		t.Fatal("metadata was not created from the AST")
	}

	want := map[string]calls.PermissionScopeLevels{"attest": {"contents": calls.PermissionLevelRead}}
	if diff := cmp.Diff(want, fromFile.JobPermissions); diff != "" {
		t.Fatal("metadata from disk is unexpected. diff:\n" + diff)
	}
	if diff := cmp.Diff(fromFile.JobPermissions, fromNode.JobPermissions); diff != "" {
		t.Fatal("disk and AST routes disagree. diff:\n" + diff)
	}
}

func TestCacheDoesNotRegisterSiblingRepository(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "repo")
	cache := newCapturedWorkflowCache(t, root, root, nil)
	for _, other := range []string{root + "-other", filepath.Join(root, "..", "elsewhere")} {
		file := filepath.Join(other, "workflow.yaml")
		if spec, ok := cache.convWorkflowPathToSpec(file); ok {
			t.Fatalf("registered outside repository: %q as %q", file, spec)
		}
	}
	file := filepath.Join(root, ".github", "workflows", "test.yaml")
	if spec, ok := cache.convWorkflowPathToSpec(file); !ok || spec != "./.github/workflows/test.yaml" {
		t.Fatalf("rejected repository workflow: spec=%q ok=%v", spec, ok)
	}
}

func TestCacheWithoutRepositoryIgnoresInvalidReference(t *testing.T) {
	t.Parallel()
	cache := NewCache("", nil)
	if _, err := cache.Resolved().LookupForCall("./"); err != nil {
		t.Fatal(err)
	}
	if _, _, found := cache.Lookup("./"); found {
		t.Fatal("a cache without a repository must not retain metadata")
	}
}

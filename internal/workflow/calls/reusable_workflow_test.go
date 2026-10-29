package calls

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"github.com/google/go-cmp/cmp"
)

func TestReusableWorkflowUnmarshalOK(t *testing.T) {
	tests := []struct {
		what string
		src  string
		want *Metadata
	}{
		{
			what: "minimal",
			src: `
			on:
			  workflow_call:
			    inputs:
			      i:
			        type: string
			    outputs:
			      o:
			        value: foo
			    secrets:
			      x:
			`,
			want: &Metadata{
				Inputs: Inputs{
					"i": {"i", false, exprlang.StringType{}},
				},
				Outputs: Outputs{
					"o": {"o"},
				},
				Secrets: Secrets{
					"x": {"x", false},
				},
			},
		},
		{
			what: "empty",
			src: `
			on:
			  workflow_call:
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty values",
			src: `
			on:
			  workflow_call:
			    inputs:
			    outputs:
			    secrets:
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "inputs",
			src: `
			on:
			  workflow_call:
			    inputs:
			      a:
			        type: string
			      b:
			        type: number
			      c:
			        type: boolean
			      d:
			      e:
			        type: string
			        required: false
			      f:
			        type: string
			        required: true
			      g:
			        type: string
			        default: abc
			      h:
			        type: string
			        default: abc
			        required: true
			      i:
			        required: true
			`,
			want: &Metadata{
				Inputs: Inputs{
					"a": {"a", false, exprlang.StringType{}},
					"b": {"b", false, exprlang.NumberType{}},
					"c": {"c", false, exprlang.BoolType{}},
					"d": {"d", false, exprlang.AnyType{}},
					"e": {"e", false, exprlang.StringType{}},
					"f": {"f", true, exprlang.StringType{}},
					"g": {"g", false, exprlang.StringType{}},
					"h": {"h", false, exprlang.StringType{}},
					"i": {"i", true, exprlang.AnyType{}},
				},
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "outputs",
			src: `
			on:
			  workflow_call:
			    inputs:
			      i:
			        type: string
			    outputs:
			      o:
			        value: foo
			    secrets:
			      x:
			`,
			want: &Metadata{
				Inputs: Inputs{
					"i": {"i", false, exprlang.StringType{}},
				},
				Outputs: Outputs{
					"o": {"o"},
				},
				Secrets: Secrets{
					"x": {"x", false},
				},
			},
		},
		{
			what: "secrets",
			src: `
			on:
			  workflow_call:
			    secrets:
			      x:
			      y:
			        required: false
			      z:
			        required: true
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: Secrets{
					"x": {"x", false},
					"y": {"y", false},
					"z": {"z", true},
				},
			},
		},
		{
			what: "empty event in scalar node",
			src: `
			on: workflow_call
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty event in sequence node",
			src: `
			on: [workflow_call]
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty event in sequence node with other events",
			src: `
			on: [pull_request, workflow_call, push]
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "upper case",
			src: `
			on:
			  workflow_call:
			    inputs:
			      MY_INPUT1:
			        type: string
			      MY_INPUT2:
			        type: number
			    outputs:
			      MY_OUTPUT1:
			        value: foo
			      MY_OUTPUT2:
			        value: foo
			    secrets:
			      MY_SECRET1:
			      MY_SECRET2:
			        required: true
			`,
			want: &Metadata{
				Inputs: Inputs{
					"my_input1": {"MY_INPUT1", false, exprlang.StringType{}},
					"my_input2": {"MY_INPUT2", false, exprlang.NumberType{}},
				},
				Outputs: Outputs{
					"my_output1": {"MY_OUTPUT1"},
					"my_output2": {"MY_OUTPUT2"},
				},
				Secrets: Secrets{
					"my_secret1": {"MY_SECRET1", false},
					"my_secret2": {"MY_SECRET2", true},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			src := strings.TrimSpace(tc.src)
			src = strings.ReplaceAll(src, "\t", "")

			m, err := ParseMetadata([]byte(src))
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(m, tc.want); diff != "" {
				t.Fatal("Parse result is unexpected. diff:\n" + diff)
			}
		})
	}
}

func TestReusableWorkflowUnmarshalOnNodeNotFound(t *testing.T) {
	src := "hello: world"
	_, err := ParseMetadata([]byte(src))
	if err == nil {
		t.Fatal("Error did not happen")
	}
	if msg := err.Error(); msg != "\"on:\" is not found" {
		t.Fatal("Unexpected error:", msg)
	}
}

func TestReusableWorkflowUnmarshalEventNotFound(t *testing.T) {
	tests := []struct {
		what string
		src  string
		line int
		col  int
	}{
		{
			what: "scalar",
			src: `
			on: push
			`,
			line: 1,
			col:  5,
		},
		{
			what: "sequence",
			src: `
			on: [push, pull_request]
			`,
			line: 1,
			col:  5,
		},
		{
			what: "empty sequence",
			src: `
			on: []
			`,
			line: 1,
			col:  5,
		},
		{
			what: "mapping",
			src: `
			on:
			  push:
			  pull_request:
			`,
			line: 2,
			col:  3,
		},
		{
			what: "empty mapping",
			src: `
			on: {}
			`,
			line: 1,
			col:  5,
		},
		{
			what: "null",
			src: `
			on:
			`,
			line: 1,
			col:  4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			src := strings.TrimSpace(tc.src)
			src = strings.ReplaceAll(src, "\t", "")

			_, err := ParseMetadata([]byte(src))
			if err == nil {
				t.Fatal("Error did not happen")
			}

			msg := err.Error()
			if !strings.Contains(msg, "\"workflow_call\" event trigger is not found in \"on:\"") {
				t.Fatal("Unexpected error:", msg)
			}
			loc := fmt.Sprintf("line:%d, column:%d", tc.line, tc.col)
			if !strings.Contains(msg, loc) {
				t.Fatalf("location is not %q: %s", loc, msg)
			}
		})
	}
}

var testReusableWorkflowWantedMetadata *Metadata = &Metadata{
	Inputs: Inputs{
		"input1": {"input1", false, exprlang.StringType{}},
		"input2": {"input2", true, exprlang.BoolType{}},
	},
	Outputs: Outputs{
		"output1": {"output1"},
	},
	Secrets: Secrets{
		"secret1": {"secret1", false},
		"secret2": {"secret2", true},
	},
}

func TestReusableWorkflowCacheFindMetadataOK(t *testing.T) {
	proj := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
	c := newCapturedWorkflowCache(t, proj, "", nil)

	m, err := c.FindMetadata("./ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(m, testReusableWorkflowWantedMetadata); diff != "" {
		t.Fatal(diff)
	}

	m2, err := c.FindMetadata("./ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m != m2 {
		t.Error("metadata is not cached")
	}
}

func TestReusableWorkflowCacheFindMetadataError(t *testing.T) {
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
			proj := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
			c := newCapturedWorkflowCache(t, proj, "", nil)
			_, err := c.FindMetadata(tc.spec)
			if err == nil {
				t.Fatal("no error happened")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("unexpected error. wanted %q but got %q", tc.want, msg)
			}
			// Each caller receives the cached failure at its own lookup.
			m, err := c.FindMetadata(tc.spec)
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
	p := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
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
			c := newCapturedWorkflowCache(t, tc.proj, "", nil)
			m, err := c.FindMetadata(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if m != nil {
				t.Fatal("metadata should be nil:", m)
			}
			m, err = c.FindMetadata(tc.spec)
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
	tests := []struct {
		what   string
		inputs map[string]*workflowlang.WorkflowCallEventInput
		want   Inputs
	}{
		{
			what: "type of inputs",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"string_input":  {Type: workflowlang.WorkflowCallEventInputTypeString},
				"number_input":  {Type: workflowlang.WorkflowCallEventInputTypeNumber},
				"bool_input":    {Type: workflowlang.WorkflowCallEventInputTypeBoolean},
				"unknown_input": {},
			},
			want: Inputs{
				"string_input":  {"string_input", false, exprlang.StringType{}},
				"number_input":  {"number_input", false, exprlang.NumberType{}},
				"bool_input":    {"bool_input", false, exprlang.BoolType{}},
				"unknown_input": {"unknown_input", false, exprlang.AnyType{}},
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
			want: Inputs{
				"unspecified":          {"unspecified", false, exprlang.AnyType{}},
				"not_required":         {"not_required", false, exprlang.AnyType{}},
				"required":             {"required", true, exprlang.AnyType{}},
				"required_but_default": {"required_but_default", false, exprlang.AnyType{}},
				"expression":           {"expression", false, exprlang.AnyType{}},
			},
		},
		{
			what:   "empty",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{},
			want:   Inputs{},
		},
		{
			what: "upper case input",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"MY_INPUT": {Type: workflowlang.WorkflowCallEventInputTypeString},
			},
			want: Inputs{
				"my_input": {"MY_INPUT", false, exprlang.StringType{}},
			},
		},
		{
			what: "upper case inputs",
			inputs: map[string]*workflowlang.WorkflowCallEventInput{
				"MY_INPUT": {},
			},
			want: Inputs{
				"my_input": {"MY_INPUT", false, exprlang.AnyType{}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
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
	tests := [][]string{
		{},
		{"foo"},
		{"a", "b", "c"},
		{"A", "B", "C"},
	}
	for _, outputs := range tests {
		t.Run(fmt.Sprintf("%s", outputs), func(t *testing.T) {
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

			want := Outputs{}
			for _, o := range outputs {
				want[strings.ToLower(o)] = &Output{o}
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

			want := Secrets{}
			for n, r := range secrets {
				want[strings.ToLower(n)] = &Secret{
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

	m1 := &Metadata{}
	c = newCapturedWorkflowCache(t, proj, cwd, nil)
	c.Store("./dir/workflow.yaml", m1, nil)
	c.WriteWorkflowCallEvent(filepath.Join("dir", "workflow.yaml"), &workflowlang.WorkflowCallEvent{})
	m2, _, ok := c.Lookup("./dir/workflow.yaml")
	if !ok {
		t.Fatal("Metadata was not created for ./dir/workflow.yaml")
	}
	if m1 != m2 {
		t.Fatalf("Metadata was not cached for ./dir/workflow.yaml %v vs %v", m1, m2)
	}
}

func TestReusableWorkflowMetadataCacheFindOneMetadataConcurrently(t *testing.T) {
	n := 10
	cwd := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
	proj := cwd
	c := newCapturedWorkflowCache(t, proj, cwd, nil)
	ret := make(chan *Metadata)
	err := make(chan error)

	for range n {
		go func() {
			m, e := c.FindMetadata("./ok.yaml")
			if e != nil {
				err <- e
				return
			}
			ret <- m
		}()
	}

	ms := []*Metadata{}
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
	n := 10
	cwd := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
	proj := cwd
	c := newCapturedWorkflowCache(t, proj, cwd, nil)
	ret := make(chan struct{})
	err := make(chan error)

	fromFile := func() {
		if _, e := c.FindMetadata("./ok.yaml"); e != nil {
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
	c := newNullLocalReusableWorkflowCache(io.Discard)
	e := &workflowlang.WorkflowCallEvent{Inputs: []*workflowlang.WorkflowCallEventInput{}}

	// This should do nothing
	c.WriteWorkflowCallEvent(filepath.Join("foo", "test.yaml"), e)

	m, err := c.FindMetadata("./foo/test.yaml")
	if m != nil {
		t.Errorf("metadata should never be found with null cache: %v", m)
	}
	if err != nil {
		t.Errorf("error should not happen since the cache simply doesn't hit: %v", err)
	}
}

func TestReusableWorkflowCacheFactory(t *testing.T) {
	cwd := filepath.Join("path", "to", "project1")
	f := NewCacheFactory(cwd, nil)

	p1 := cwd
	c1 := f.GetCache(p1)

	p2 := filepath.Join("path", "to", "project2")
	c2 := f.GetCache(p2)
	if c1 == c2 {
		t.Errorf("Different cache was not created: %v", c1)
	}

	c3 := f.GetCache(p1)
	if c1 != c3 {
		t.Errorf("Same cache was not used: %v vs %v", c1, c3)
	}

	c4 := f.GetCache("")
	if c4.root != "" {
		t.Errorf("Null cache should be returned when project is nil: %v", c4)
	}
}

func TestReusableWorkflowMetadataJobPermissions(t *testing.T) {
	readAll := PermissionScopeLevels{}
	for s, vs := range allPermissionScopes {
		if slices.Contains(vs, "read") {
			readAll[s] = PermissionLevelRead
		}
	}

	want := map[string]PermissionScopeLevels{
		"inherits":      {"contents": PermissionLevelRead},
		"Override":      {"pull-requests": PermissionLevelWrite},
		"all-read":      readAll,
		"aliased-perms": {"contents": PermissionLevelRead},
		"base":          {"id-token": PermissionLevelWrite},
		"duplicate":     {"id-token": PermissionLevelWrite},
	}

	proj := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
	c := newCapturedWorkflowCache(t, proj, "", nil)
	m, err := c.FindMetadata("./permissions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(want, m.JobPermissions); diff != "" {
		t.Fatal(diff)
	}
}

func TestReusableWorkflowMetadataJobPermissionsFromWorkflowNode(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata", "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	w, errs := workflowparser.Parse(src)
	if w == nil {
		t.Fatal("workflow was not parsed:", errs)
	}

	fromFile, err := ParseMetadata(src)
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
				for _, cache := range []*Cache{fileCache, astCache} {
					for pass := range 2 {
						metadata, err := cache.FindMetadata("./callee.yaml")
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

func TestReusableWorkflowMetadataInvalidAnchorName(t *testing.T) {
	src := []byte(`on: workflow_call
jobs:
  first: &build+job
    runs-on: ubuntu-latest
    steps:
      - run: echo test
  second: *build+job
`)
	_, err := ParseMetadata(src)
	if err == nil || !strings.Contains(err.Error(), `line:3, column:10: anchor name "build+job"`) {
		t.Fatalf("wanted the invalid anchor diagnostic, got %v", err)
	}
}

func TestReusableWorkflowMetadataUnusedAnchorParity(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "reusable_workflow_metadata")
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
	fromFile, err := c.FindMetadata("./unused_anchor.yaml")
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

	want := map[string]PermissionScopeLevels{"attest": {"contents": PermissionLevelRead}}
	if diff := cmp.Diff(want, fromFile.JobPermissions); diff != "" {
		t.Fatal("metadata from disk is unexpected. diff:\n" + diff)
	}
	if diff := cmp.Diff(fromFile.JobPermissions, fromNode.JobPermissions); diff != "" {
		t.Fatal("disk and AST routes disagree. diff:\n" + diff)
	}
}

func TestCacheDoesNotRegisterSiblingRepository(t *testing.T) {
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
	cache := NewCacheFactory(t.TempDir(), nil).GetCache("")
	cache.IgnoreInvalid("./")
	if _, _, found := cache.Lookup("./"); found {
		t.Fatal("a cache without a repository must not retain metadata")
	}
}

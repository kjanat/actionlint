package rules_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflowcall"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestRuleWorkflowCallCheckReusableWorkflowCall(t *testing.T) {
	t.Parallel()
	cwd := filepath.Join("..", "..", "..", "..", "input", "references", "reusableworkflows", "testdata", "reusable-workflows")
	cache := newCapturedWorkflowCache(t, cwd, nil)

	for i, md := range []*calls.Metadata{
		{
			Inputs: calls.Inputs{
				"optional_input": {Name: "optional_input", Required: false, Type: definition.WorkflowCallEventInputTypeString},
				"required_input": {Name: "required_input", Required: true, Type: definition.WorkflowCallEventInputTypeString},
			},
			Outputs: calls.Outputs{
				"output": {Name: "output"},
			},
			Secrets: calls.Secrets{
				"optional_secret": {Name: "optional_secret", Required: false},
				"required_secret": {Name: "required_secret", Required: true},
			},
		},
		{
			Inputs: calls.Inputs{
				"optional_input": {Name: "OPTIONAL_INPUT", Required: false, Type: definition.WorkflowCallEventInputTypeString},
				"required_input": {Name: "REQUIRED_INPUT", Required: true, Type: definition.WorkflowCallEventInputTypeString},
			},
			Outputs: calls.Outputs{
				"output": {Name: "OUTPUT"},
			},
			Secrets: calls.Secrets{
				"optional_secret": {Name: "OPTIONAL_SECRET", Required: false},
				"required_secret": {Name: "REQUIRED_SECRET", Required: true},
			},
		},
		{
			Inputs:  calls.Inputs{},
			Outputs: calls.Outputs{},
			Secrets: calls.Secrets{},
		},
	} {
		cache.Store(fmt.Sprintf("./workflow%d.yaml", i), md)
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
			uses:    "./ok.yaml",
			inputs:  []string{"input2"},
			secrets: []string{"secret2"},
		},
		{
			what: "read broken workflow",
			uses: "./broken.yaml",
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
			t.Parallel()
			c := &definition.WorkflowCall{
				Uses:           &definition.String{Value: tc.uses, Pos: &sourcetext.Position{}},
				Inputs:         map[string]*definition.WorkflowCallInput{},
				Secrets:        map[string]*definition.WorkflowCallSecret{},
				InheritSecrets: tc.inheritSecrets,
			}
			for _, i := range tc.inputs {
				c.Inputs[strings.ToLower(i)] = &definition.WorkflowCallInput{
					Name:  &definition.String{Value: i, Pos: &sourcetext.Position{}},
					Value: &definition.String{Value: "", Pos: &sourcetext.Position{}},
				}
			}
			for _, s := range tc.secrets {
				c.Secrets[strings.ToLower(s)] = &definition.WorkflowCallSecret{
					Name:  &definition.String{Value: s, Pos: &sourcetext.Position{}},
					Value: &definition.String{Value: "", Pos: &sourcetext.Position{}},
				}
			}

			metadata, err := cache.LoadForCall(t.Context(), tc.uses)
			if err != nil {
				if len(tc.errs) != 1 || !strings.Contains(err.Error(), tc.errs[0]) {
					t.Fatalf("unexpected lookup error: %v; want %v", err, tc.errs)
				}
				return
			}
			view := workflows.Bind("call-fixture", "", &definition.Workflow{Jobs: map[string]*definition.Job{"test": {WorkflowCall: c}}}, false, "").Workflow()
			job, _ := view.Job("test")
			findings := workflowcall.CheckArguments(job.WorkflowCall, metadata)

			errs := []string{}
			for _, err := range findings {
				errs = append(errs, err.Message)
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

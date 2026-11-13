package rule

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"github.com/kjanat/go-yaml/v4"
)

func TestWorkflowExampleSourceSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		job  string
		want []string
	}{
		{name: "inline empty steps", job: "    steps: []\n", want: []string{}},
		{name: "block steps", job: "    steps:\n      - run: echo existing\n", want: []string{"echo existing"}},
		{name: "missing steps", job: "    timeout-minutes: 5\n", want: []string{"echo ok"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := WorkflowExampleSource(tt.job)
			if _, errors := parser.Parse([]byte(source)); len(errors) > 0 {
				t.Fatalf("invalid workflow: %v\n%s", errors, source)
			}
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						Run string `yaml:"run"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal([]byte(source), &workflow); err != nil {
				t.Fatalf("invalid workflow YAML: %v\n%s", err, source)
			}
			var got []string
			for _, step := range workflow.Jobs["build"].Steps {
				got = append(got, step.Run)
			}
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("steps = %v, want %v\n%s", got, tt.want, source)
			}
		})
	}
}

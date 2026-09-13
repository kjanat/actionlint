package actionlint

import (
	"io"
	"strings"
	"testing"
)

func TestSchemaNestedTimezoneAliases(t *testing.T) {
	// LoadLocation can resolve casing variants through the host filesystem.
	// Use a nonexistent zone, not lowercase UTC, to test an invalid IANA name.
	for _, tz := range []struct {
		name  string
		valid bool
	}{{"UTC", true}, {"Etc/UTC", true}, {"Universal", true}, {"Europe/Amsterdam", true}, {"Local", false}, {"local", false}, {"Invalid/Timezone", false}} {
		t.Run(tz.name, func(t *testing.T) {
			rule := NewRuleEvents()
			rule.checkTimezone(&String{Value: tz.name, Pos: &Pos{Line: 1, Col: 1}})
			if valid := len(rule.Errs()) == 0; valid != tz.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tz.valid, rule.Errs())
			}
		})
	}
}

func TestSchemaNestedWaitAllStaticValue(t *testing.T) {
	for _, value := range []struct {
		text  string
		valid bool
	}{{"", true}, {"true", true}, {"True", true}, {"false", false}, {"${{ true }}", false}, {"${{ fromJSON('true') }}", false}, {"${{ github.event_name == 'push' }}", false}} {
		t.Run(value.text, func(t *testing.T) {
			_, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - wait-all: " + value.text + "\n"))
			if valid := len(errs) == 0; valid != value.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, value.valid, errs)
			}
		})
	}
}

func TestSchemaNestedScheduleRequiresCron(t *testing.T) {
	src := "on:\n  schedule:\n    - cron: '0 0 * * *'\n    - timezone: Europe/Amsterdam\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"
	_, errs := Parse([]byte(src))
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `"cron" is missing`) || errs[0].Line != 4 {
		t.Fatalf("wanted missing cron diagnostic at second schedule entry, got %v", errs)
	}
}

func TestSchemaNestedImageVersionFilters(t *testing.T) {
	for _, filters := range []string{
		"types: ready\n    names: build-image\n    versions: '1.*'",
		"types: [created, ready, deleted]\n    names: [build-image]\n    versions: ['1.*']",
	} {
		src := "on:\n  image_version:\n    " + filters + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"
		w, errs := Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatalf("schema-supported image filters rejected: %v", errs)
		}
		rule := NewRuleEvents()
		if err := rule.VisitWorkflowPre(w); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("schema-supported activity types rejected: %v", errs)
		}
	}
}

func TestSchemaNestedImageVersionRejectsUnknownActivity(t *testing.T) {
	src := "on:\n  image_version:\n    types: unavailable\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"
	w, errs := Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 1 || !strings.Contains(errs[0].Message, `invalid activity type "unavailable"`) {
		t.Fatalf("wanted invalid image activity diagnostic, got %v", errs)
	}
}

func TestSchemaNestedDisabledService(t *testing.T) {
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    services:\n      disabled:\n        image: ''\n    steps:\n      - run: echo test\n"
	_, errs := Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatalf("documented empty service image rejected: %v", errs)
	}
}

func TestSchemaNestedEmptyChoiceOption(t *testing.T) {
	src := "on:\n  workflow_dispatch:\n    inputs:\n      selection:\n        type: choice\n        options: ['', selected]\n        default: ''\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"
	w, errs := Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatalf("valid empty choice option/default rejected: %v", errs)
	}
}

func TestSchemaNestedStackedPullRequests(t *testing.T) {
	w, errs := Parse([]byte("on:\n  pull_request:\n    types: stacked\n  pull_request_target:\n    types: stacked\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestSchemaNestedWorkflowDescription(t *testing.T) {
	w, errs := Parse([]byte("description: Build reusable binaries\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if w.Description == nil || w.Description.Value != "Build reusable binaries" {
		t.Fatalf("description was not preserved: %#v", w.Description)
	}
}

func TestSchemaNestedJobExpressions(t *testing.T) {
	for _, tc := range []struct {
		name, job, want string
	}{
		{"cancel timeout literal", "cancel-timeout-minutes: 2.5", ""},
		{"cancel timeout strategy context", "cancel-timeout-minutes: ${{ strategy.job-index }}", ""},
		{"cancel timeout forbidden context", "cancel-timeout-minutes: ${{ secrets.TIMEOUT }}", "secrets"},
		{"cancel timeout wrong type", "cancel-timeout-minutes: ${{ true }}", "must be number"},
		{"cancel timeout wrong shape", "cancel-timeout-minutes: []", "float value"},
		{"include expression object", "strategy:\n      matrix:\n        include:\n          - ${{ fromJSON('{\"os\":\"ubuntu-latest\"}') }}", ""},
		{"include expression scalar", "strategy:\n      matrix:\n        include:\n          - ${{ true }}", "object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    " + tc.job + "\n    steps:\n      - run: echo test\n"
			errs, err := linter.Lint("test.yaml", []byte(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted diagnostic containing %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestSchemaNestedStructuredExpressions(t *testing.T) {
	for _, tc := range []struct {
		name, field string
	}{
		{"strategy", "strategy: ${{ fromJSON('{\"matrix\":{\"os\":[\"ubuntu-latest\"]}}') }}"},
		{"defaults", "defaults:\n      run: ${{ fromJSON('{\"shell\":\"bash\"}') }}"},
		{"container", "container: ${{ fromJSON('{\"image\":\"ubuntu:latest\"}') }}"},
		{"service", "services:\n      redis: ${{ fromJSON('{\"image\":\"redis:latest\"}') }}"},
		{"environment", "environment: ${{ fromJSON('{\"name\":\"staging\"}') }}"},
		{"concurrency", "concurrency: ${{ fromJSON('{\"group\":\"deploy\",\"cancel-in-progress\":false}') }}"},
		{"queue", "concurrency:\n      group: deploy\n      queue: ${{ vars.QUEUE }}"},
		{"snapshot", "snapshot: ${{ fromJSON('{\"image-name\":\"build-image\"}') }}"},
		{"ports", "container:\n      image: ubuntu:latest\n      ports: ${{ fromJSON('[\"8080:80\"]') }}"},
		{"volumes", "container:\n      image: ubuntu:latest\n      volumes: ${{ fromJSON('[\"/src:/src\"]') }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			src := "on: push\njobs:\n  test:\n    runs-on: ${{ fromJSON('{\"labels\":\"ubuntu-latest\"}') }}\n    " + tc.field + "\n    steps:\n      - uses: docker://alpine:3\n        with: ${{ fromJSON('{\"args\":\"echo test\"}') }}\n"
			errs, err := linter.Lint("test.yaml", []byte(src), nil)
			if err != nil || len(errs) != 0 {
				t.Fatalf("schema-supported expression object rejected: %v %v", err, errs)
			}
		})
	}
}

func TestSchemaNestedStepIDBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id    string
		valid bool
	}{{"_user", true}, {"__reserved", false}, {strings.Repeat("a", 99), true}, {strings.Repeat("a", 100), false}} {
		t.Run(tc.id, func(t *testing.T) {
			w, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - id: " + tc.id + "\n        run: echo test\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleID()
			if err := rule.VisitJobPre(w.Jobs["test"]); err != nil {
				t.Fatal(err)
			}
			if err := rule.VisitStep(w.Jobs["test"].Steps[0]); err != nil {
				t.Fatal(err)
			}
			if valid := len(rule.Errs()) == 0; valid != tc.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tc.valid, rule.Errs())
			}
		})
	}
}

func TestSchemaNestedQuotedScalarTags(t *testing.T) {
	for _, field := range []string{
		"cancel-timeout-minutes: !!int \"300\"",
		"timeout-minutes: !!float \"3.0\"",
		"continue-on-error: !!bool \"true\"",
		"name: !!int \"300\"",
	} {
		t.Run(field, func(t *testing.T) {
			_, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    " + field + "\n    steps:\n      - run: echo test\n"))
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "tag of a quoted or block scalar") {
				t.Fatalf("wanted scalar tag/style diagnostic, got %v", errs)
			}
		})
	}
}

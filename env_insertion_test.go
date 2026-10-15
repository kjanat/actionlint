package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestEnvironmentMappingInsertions(t *testing.T) {
	for _, scope := range []struct{ name, source string }{
		{"workflow", "env:\n  BASE: keep\n  ${{ insert }}: %s\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"},
		{"job", "jobs:\n  check:\n    runs-on: ubuntu-latest\n    env:\n      BASE: keep\n      ${{ insert }}: %s\n    steps: [{run: echo ok}]\n"},
		{"step", "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          BASE: keep\n          ${{ insert }}: %s\n"},
		{"container", "jobs:\n  check:\n    runs-on: ubuntu-latest\n    container:\n      image: node:24\n      env:\n        BASE: keep\n        ${{ insert }}: %s\n    steps: [{run: echo ok}]\n"},
		{"service", "jobs:\n  check:\n    runs-on: ubuntu-latest\n    services:\n      database:\n        image: postgres:17\n        env:\n          BASE: keep\n          ${{ insert }}: %s\n    steps: [{run: echo ok}]\n"},
	} {
		for _, tc := range []struct{ name, value, want string }{
			{"unknown object", "${{ fromJSON(vars.GIT) }}", ""},
			{"known object", `${{ fromJSON('{"NAME":"value","NUMBER":42,"BOOL":false,"EMPTY":null}') }}`, ""},
			{"empty object expression", "${{ fromJSON('{}') }}", ""},
			{"empty literal object", "{}", ""},
			{"literal mapping", "{VALUE: '${{ github.repository }}'}", ""},
			{"nested insertion", "{'${{ insert }}': {VALUE: '${{ github.repository }}'}}", ""},
			{"literal expression result", `${{ fromJSON('{"VALUE":"${{ missing }}"}') }}`, ""},
			{"array expression", "${{ fromJSON('[]') }}", "env must be object but found array"},
			{"scalar expression", "${{ 'value' }}", "env must be object but found string"},
			{"invalid inserted value", `${{ fromJSON('{"VALUE":[]}') }}`, "env.VALUE must be string but found array"},
			{"undefined source", "${{ fromJSON(missing.VALUE) }}", `undefined variable "missing"`},
			{"undefined inserted expression", "{VALUE: '${{ missing }}'}", `undefined variable "missing"`},
			{"invalid inserted name", "{'BAD NAME': value}", `environment variable name "BAD NAME" is invalid`},
			{"invalid literal array", "[]", "mapping node is expected"},
		} {
			t.Run(scope.name+"/"+tc.name, func(t *testing.T) {
				source := "on: push\n" + fmt.Sprintf(scope.source, tc.value)
				errs := lintCredentialsExpressionTest(t, source)
				if tc.want == "" {
					if len(errs) != 0 {
						t.Fatalf("unexpected diagnostics: %v", errs)
					}
				} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
					t.Fatalf("want one diagnostic containing %q, got %v", tc.want, errs)
				}
			})
		}
	}
}

func TestEnvironmentInsertionDirectiveBoundary(t *testing.T) {
	const source = `on: push
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
        env:
          %s: %s
`
	for _, tc := range []struct{ name, key, value, want string }{
		{"compact", "${{insert}}", "${{ fromJSON(vars.GIT) }}", ""},
		{"ordinary uppercase identifier", "${{ INSERT }}", "plain", `undefined variable "INSERT"`},
		{"extra whitespace", "'  ${{  insert  }}  '", "${{ fromJSON(vars.GIT) }}", ""},
		{"literal key expression", "${{ 'insert' }}", "plain", ""},
		{"ordinary dynamic key", "${{ vars.NAME }}", "plain", ""},
		{"ordinary value", "VALUE", "${{ insert }}", `undefined variable "insert"`},
		{"property access", "${{ insert.VALUE }}", "plain", `undefined variable "insert"`},
		{"interpolation", "PREFIX_${{ insert }}", "plain", `undefined variable "insert"`},
		{"multiple expressions", "'${{ insert }}${{ vars.NAME }}'", "plain", `undefined variable "insert"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := lintCredentialsExpressionTest(t, fmt.Sprintf(source, tc.key, tc.value))
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("want one diagnostic containing %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestEnvironmentInsertionAvailabilityAndPosition(t *testing.T) {
	const source = `on: push
env:
  ${{ insert }}: ${{ fromJSON(env.DATA) }}
jobs:
  check:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
	errs := lintCredentialsExpressionTest(t, source)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `context "env" is not allowed`) || errs[0].Line != 3 || errs[0].Column != 31 {
		t.Fatalf("context restriction or source position lost: %v", errs)
	}
}

func TestEnvironmentInsertionInvocationSafety(t *testing.T) {
	for _, tc := range []struct {
		name, value           string
		checkout, shell, path bool
	}{
		{"unknown", "${{ fromJSON(vars.ENV) }}", true, true, true},
		{"safe literals", "{VALUE: text}", false, false, false},
		{"constant object", `${{ fromJSON('{"VALUE":"text"}') }}`, false, false, false},
		{"empty object", `${{ fromJSON('{}') }}`, false, false, false},
		{"constant path", `${{ fromJSON('{"PATH":"custom"}') }}`, true, false, true},
		{"constant startup", `${{ fromJSON('{"BASH_ENV":"startup.sh"}') }}`, false, true, false},
		{"constant empty startup", `${{ fromJSON('{"BASH_ENV":""}') }}`, false, false, false},
		{"constant loader", `${{ fromJSON('{"LD_PRELOAD":"custom.so"}') }}`, true, true, false},
		{"constant empty loader", `${{ fromJSON('{"LD_PRELOAD":""}') }}`, false, false, false},
		{"constant checkout wrapper", `${{ fromJSON('{"GIT_EXEC_PATH":"custom"}') }}`, true, false, false},
		{"evaluated value remains data", `${{ fromJSON('{"BASH_ENV":"${{ '''' }}"}') }}`, false, true, false},
		{"evaluated name remains data", `${{ fromJSON('{"${{ ''PATH'' }}":"custom"}') }}`, false, false, false},
		{"invalid object", `${{ fromJSON('[]') }}`, true, true, true},
		{"nested path", "{'${{ insert }}': {PATH: custom}}", true, false, true},
		{"shell startup", "{BASH_ENV: startup.sh}", false, true, false},
		{"checkout wrapper", "{GIT_EXEC_PATH: custom}", true, false, false},
		{"loader", "{LD_PRELOAD: custom.so}", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\nenv:\n  ${{ insert }}: " + tc.value + "\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			env := workflow.Env
			if len(env.Vars) != 0 || len(env.Insertions) != 1 {
				t.Fatalf("directive parsed as an environment variable: %+v", env)
			}
			if got := checkoutEnvironmentUnknown(env, platformKindMacOrLinux); got != tc.checkout {
				t.Errorf("checkout environment unknown: %v, want %v", got, tc.checkout)
			}
			if got := shellEnvironmentUnknown(env); got != tc.shell {
				t.Errorf("shell environment unknown: %v, want %v", got, tc.shell)
			}
			if got := shellPathUnknown(env); got != tc.path {
				t.Errorf("shell path unknown: %v, want %v", got, tc.path)
			}
		})
	}
}

func TestEnvironmentInsertionAliases(t *testing.T) {
	const source = `on: push
env: &shared
  SHARED: value
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
        env:
          ${{ insert }}: *shared
`
	if errs := lintCredentialsExpressionTest(t, source); len(errs) != 0 {
		t.Fatalf("valid mapping alias rejected: %v", errs)
	}
	_, errs := Parse([]byte(`on: push
env: &recursive
  ${{ insert }}: *recursive
jobs:
  check:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`))
	for _, err := range errs {
		if strings.Contains(err.Message, "recursive alias") {
			return
		}
	}
	t.Fatalf("recursive alias was not rejected: %v", errs)
}

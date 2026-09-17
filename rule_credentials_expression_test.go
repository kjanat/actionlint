package actionlint

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestCredentialsKnownMappingExpressions(t *testing.T) {
	for _, scope := range []struct {
		name, field, where  string
		line, column        int
		container, services bool
	}{
		{"container", "    container: %s\n", `"container" section`, 5, 16, true, false},
		{"service container", "    services:\n      registry: %s\n", `"registry" service`, 6, 17, true, false},
		{"services", "    services: %s\n", `"registry" service`, 5, 15, true, true},
		{"credentials", "    container:\n      image: node:22\n      credentials: %s\n", `"container" section`, 7, 20, false, false},
		{"service credentials", "    services:\n      registry:\n        image: node:22\n        credentials: %s\n", `"registry" service`, 8, 22, false, false},
	} {
		for _, tc := range []struct{ name, credentials, kind string }{
			{"password", `{"username":"user","password":"plain"}`, "credentials"},
			{"case insensitive password", `{"Username":"user","Password":"plain"}`, "credentials"},
			{"boolean password", `{"username":"user","password":true}`, "credentials"},
			{"numeric password", `{"username":"user","password":42}`, "credentials"},
			{"password without username", `{"password":"plain"}`, "credentials"},
			{"literal secret expression", `{"username":"user","password":"${{ secrets.PASSWORD }}"}`, "credentials"},
			{"literal computed expression", `{"username":"user","password":"${{ fromJSON('{}') }}"}`, "credentials"},
			// The evaluated schema permits omitted credential fields; present values remain nonempty.
			// https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/workflow-v1.0.json#L2686-L2702
			{"empty credentials", `{}`, ""},
			{"username only", `{"username":"user"}`, ""},
			{"invalid credentials", `[]`, "expression"},
			{"invalid password", `{"username":"user","password":{}}`, "expression"},
			{"null password", `{"username":"user","password":null}`, "expression"},
			{"empty password", `{"username":"user","password":""}`, "expression"},
			{"unknown credentials", "", ""},
		} {
			t.Run(scope.name+"/"+tc.name, func(t *testing.T) {
				value := tc.credentials
				if scope.container {
					value = `{"image":"node:22","Credentials":` + value + `}`
				}
				if scope.services {
					value = `{"registry":` + value + `}`
				}
				expression := "${{ fromJSON('" + strings.ReplaceAll(value, "'", "''") + "') }}"
				if tc.credentials == "" {
					expression = "${{ fromJSON(vars.DATA) }}"
					if !scope.container {
						expression = "${{ fromJSON(secrets.CREDENTIALS) }}"
					}
				}
				source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n" + fmt.Sprintf(scope.field, expression) + "    steps:\n      - run: echo ok\n"
				errs := lintCredentialsExpressionTest(t, source)
				if tc.kind == "" {
					if len(errs) != 0 {
						t.Fatalf("unexpected diagnostics: %v", errs)
					}
					return
				}
				if len(errs) != 1 || errs[0].Kind != tc.kind {
					t.Fatalf("want one %s diagnostic, got %v", tc.kind, errs)
				}
				if tc.kind == "credentials" {
					want := `"password" section in ` + scope.where + " should be specified via secrets. do not put password value directly"
					if got := errs[0]; got.Message != want || got.Line != scope.line || got.Column != scope.column || got.Filepath != "workflow.yml" {
						t.Fatalf("credential diagnostic lost message or source position: %v", got)
					}
				}
			})
		}
	}
}

func TestCredentialsKnownPasswordExpressions(t *testing.T) {
	for _, tc := range []struct{ name, value, kind string }{
		{"literal password", "plain", "credentials"},
		{"literal string expression", "${{ 'plain' }}", "credentials"},
		{"literal JSON password", `${{ fromJSON('"plain"') }}`, "credentials"},
		{"literal expression data", `${{ fromJSON('"${{ secrets.PASSWORD }}"') }}`, "credentials"},
		{"boolean expression", "${{ true }}", "credentials"},
		{"numeric expression", "${{ 42 }}", "credentials"},
		{"secret password", "${{ secrets.PASSWORD }}", ""},
		{"computed secret password", "${{ fromJSON(secrets.CREDENTIALS).password }}", ""},
		{"invalid known type", "${{ fromJSON('{}') }}", "expression"},
		{"invalid null", "${{ null }}", "expression"},
		{"malformed expression", "${{ fromJSON( }}", "expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    container:\n      image: node:22\n      credentials:\n        username: user\n        password: " + tc.value + "\n    steps:\n      - run: echo ok\n"
			errs := lintCredentialsExpressionTest(t, source)
			if tc.kind == "" {
				if len(errs) != 0 {
					t.Fatalf("unknown secret value rejected: %v", errs)
				}
			} else if len(errs) != 1 || errs[0].Kind != tc.kind || errs[0].Line != 9 {
				t.Fatalf("want one %s diagnostic at password, got %v", tc.kind, errs)
			}
		})
	}
}

func TestContainerExpressionImageContract(t *testing.T) {
	// The converter returns no job container for an explicit empty image.
	// https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/WorkflowTemplateConverter.cs#L1203-L1218
	for _, tc := range []struct {
		name, value string
		missing     bool
	}{
		{"empty string", "${{ '' }}", false},
		{"empty JSON string", `${{ fromJSON('""') }}`, false},
		{"empty image property", `${{ fromJSON('{"image":""}') }}`, false},
		{"empty image with partial credentials", `${{ fromJSON('{"image":"","credentials":{"username":"user"}}') }}`, false},
		// Requiring the property is actionlint policy, separate from allowing an explicit empty value.
		{"missing image property", `${{ fromJSON('{}') }}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    container: " + tc.value + "\n    steps:\n      - run: echo ok\n"
			errs := lintCredentialsExpressionTest(t, source)
			if !tc.missing {
				if len(errs) != 0 {
					t.Fatalf("explicit empty image rejected: %v", errs)
				}
				return
			}
			if len(errs) != 1 || errs[0].Kind != "expression" || !strings.Contains(errs[0].Message, `requires property "image"`) {
				t.Fatalf("want one missing-image diagnostic, got %v", errs)
			}
		})
	}
}

func lintCredentialsExpressionTest(t *testing.T, source string) []*Error {
	t.Helper()
	linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
	if err != nil {
		t.Fatal(err)
	}
	errs, err := linter.Lint("workflow.yml", []byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	return errs
}

package config_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"github.com/google/go-cmp/cmp"
)

func TestConfigParseSelfHostedRunnerOK(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what   string
		input  string
		labels []string
	}{
		{
			what:   "empty config",
			input:  "",
			labels: nil,
		},
		{
			what:   "empty self-hosted-runner",
			input:  "self-hosted-runner:\n",
			labels: nil,
		},
		{
			what:   "null self-hosted-runner labels",
			input:  "self-hosted-runner:\n  labels:",
			labels: nil,
		},
		{
			what:   "empty self-hosted-runner labels",
			input:  "self-hosted-runner:\n  labels: []",
			labels: []string{},
		},
		{
			what:   "self-hosted-runner labels",
			input:  "self-hosted-runner:\n  labels: [foo, bar]",
			labels: []string{"foo", "bar"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(repoconfig.RunnerLabels(c.Facts()), tc.labels); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestConfigParseConfigSecrets(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what    string
		input   string
		secrets []string
	}{
		{
			what:    "empty config",
			input:   "",
			secrets: nil,
		},
		{
			what:    "null config-secrets",
			input:   "config-secrets:",
			secrets: nil,
		},
		{
			what:    "empty config-secrets",
			input:   "config-secrets: []",
			secrets: []string{},
		},
		{
			what:    "config-secrets",
			input:   "config-secrets: [FOO, BAR]",
			secrets: []string{"FOO", "BAR"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(repoconfig.SecretNames(c.Facts()), tc.secrets); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestConfigParseError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{
			in:   `self-hosted-runner: 42`,
			want: `self-hosted-runner must be a mapping`,
		},
		{
			in: `
paths:
  foo:
    ignore: foo+
`,
			want: `ignore patterns must be an array`,
		},
		{
			in: `
paths:
  foo:
    ignore: ['(foo']
`,
			want: `error parsing regexp: missing closing )`,
		},
		{
			in: `
paths:
  foo.{txt,xml:
`,
			want: `invalid glob pattern`,
		},
		{
			in:   "assume-default-permissions: foo\n",
			want: `"assume-default-permissions" must be "restricted" or "permissive" but got "foo"`,
		},
		{
			in:   "assume-default-permissions: [restricted]\n",
			want: `"assume-default-permissions" must be "restricted" or "permissive" but got ["restricted"]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			_, err := checking.ConfigurationRules().Parse([]byte(tc.in))
			if err == nil {
				t.Fatal("no error occurred")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted error message %q to contain %q", err.Error(), tc.want)
			}
			if strings.HasPrefix(tc.in, "assume-default-permissions:") {
				var source *repoconfig.SourceError
				if !errors.As(err, &source) || source.Line != 1 || source.Column != 29 {
					t.Fatalf("wrong source position: %v", err)
				}
			}
		})
	}
}

func TestConfigPathConfigIgnores(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		msg   string
		want  bool
	}{
		{
			input: ``,
			msg:   "this is test",
			want:  false,
		},
		{
			input: `ignore: []`,
			msg:   "this is test",
			want:  false,
		},
		{
			input: `ignore: ['(is )+']`,
			msg:   "this is test",
			want:  true,
		},
		{
			input: `ignore: ['does not match', '(is )+']`,
			msg:   "this is test",
			want:  true,
		},
		{
			input: `ignore: ['does not match', 'does not match 2']`,
			msg:   "this is test",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input+"_"+tc.msg, func(t *testing.T) {
			t.Parallel()
			var c repoconfig.PathConfig
			if err := decodeConfigYAML([]byte(tc.input), &c); err != nil {
				t.Fatal(err)
			}
			have := c.Ignore.Match(tc.msg)
			if tc.want != have {
				t.Fatalf("wanted %v but got %v for message %q and input %q", tc.want, have, tc.msg, tc.input)
			}
		})
	}
}

func TestConfigIgnoreErrors(t *testing.T) {
	t.Parallel()
	src := `
paths:
  .github/workflows/**/*.yaml:
    ignore: [xxx]
  .github/workflows/*.yaml:
    ignore: [yyy]
  .github/workflows/a/*.yaml:
    ignore: [zzz]
  .github/workflows/*/b.yaml:
    ignore: [uuu]
  .github/workflows/a/b.yaml:
    ignore: [vvv]
  .github/workflows/**/x.yaml:
    ignore: [www]
  .github/workflows/**/*.{yml,yaml}:
    ignore: [ttt]
`

	var cfg repoconfig.Config
	if err := checking.ConfigurationRules().Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		msg  string
		want bool
	}{
		{"foo.yaml", "xxx", false},
		{".github/workflows/a.yaml", "xxx", true},
		{".github/workflows/a/b.yaml", "xxx", true},
		{".github/workflows/a/b/c/d/e/f/g/h.yaml", "xxx", true},
		{".github/workflows/a.yaml", "yyy", true},
		{".github/workflows/a/b.yaml", "yyy", false},
		{".github/workflows/a/b.yaml", "zzz", true},
		{".github/workflows/a/a.yaml", "zzz", true},
		{".github/workflows/b/b.yaml", "zzz", false},
		{".github/workflows/a/b.yaml", "uuu", true},
		{".github/workflows/b/b.yaml", "uuu", true},
		{".github/workflows/a/a.yaml", "uuu", false},
		{".github/workflows/a/b.yaml", "vvv", true},
		{".github/workflows/b/b.yaml", "vvv", false},
		{".github/workflows/a/a.yaml", "vvv", false},
		{".github/workflows/x.yaml", "www", true},
		{".github/workflows/a/x.yaml", "www", true},
		{".github/workflows/a/b/x.yaml", "www", true},
		{".github/workflows/a/b/c/x.yaml", "www", true},
		{".github/workflows/a/b.yaml", "this is not ignored", false},
		{".github/workflows/a.yml", "xxx", false},
		{".github/workflows/a.yml", "ttt", true},
	}

	for _, tc := range tests {
		var ignored bool
		for _, c := range cfg.PathConfigs(tc.path) {
			if c.Ignore.Match(tc.msg) {
				ignored = true
				break
			}
		}
		if ignored != tc.want {
			want, have := "not be ignored", "was ignored"
			if tc.want {
				want, have = "be ignored", "was not ignored"
			}
			t.Fatalf("error message %q with path %q should %s but actually %s", tc.msg, tc.path, want, have)
		}
	}
}

func TestConfigReadFileOK(t *testing.T) {
	t.Parallel()
	p := filepath.Join("internal", "input", "configuration", "testdata", "inheritance", "ok.yml")
	c, err := checking.ConfigurationRules().Load(p)
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{"foo", "bar"}
	if diff := cmp.Diff(repoconfig.RunnerLabels(c.Facts()), labels); diff != "" {
		t.Fatal(diff)
	}
}

func TestConfigReadFileReadError(t *testing.T) {
	t.Parallel()
	p := filepath.Join("internal", "input", "configuration", "testdata", "inheritance", "does-not-exist.yml")
	_, err := checking.ConfigurationRules().Load(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not read config file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigReadFileParseError(t *testing.T) {
	t.Parallel()
	p := filepath.Join("internal", "input", "configuration", "testdata", "inheritance", "broken.yml")
	_, err := checking.ConfigurationRules().Load(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not parse config file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigParseAssumeDefaultPermissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what  string
		input string
		want  string
	}{
		{
			what:  "key is absent",
			input: "",
			want:  "",
		},
		{
			what:  "empty value",
			input: "assume-default-permissions:\n",
			want:  "",
		},
		{
			what:  "null value",
			input: "assume-default-permissions: null\n",
			want:  "",
		},
		{
			what:  "restricted",
			input: "assume-default-permissions: restricted\n",
			want:  "read",
		},
		{
			what:  "permissive",
			input: "assume-default-permissions: permissive\n",
			want:  "write",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if got := configuredWorkflowPermissions(c); got != tc.want {
				t.Fatalf("wanted %v but have %v", tc.want, got)
			}
		})
	}
}

func TestConfigParsePolicyEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what  string
		input string
	}{
		{
			what:  "no policy",
			input: "",
		},
		{
			what:  "empty policy",
			input: "policy:\n",
		},
		{
			what:  "empty policy mapping",
			input: "policy: {}\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(repoconfig.Policy{}, c.Policy); diff != "" {
				t.Fatal(diff)
			}
			if c.RequiresCommitHash() {
				t.Fatal("\"require-commit-hash\" is enabled")
			}
		})
	}
}

func TestConfigParsePolicyUnknownKey(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().Parse([]byte(`policy:
  require-commit-hashes: true
`))
	if err == nil {
		t.Fatal("no error occurred")
	}
	var source *repoconfig.SourceError
	if !errors.As(err, &source) || source.Line != 2 || source.Column != 3 || !strings.Contains(err.Error(), "require-commit-hashes") || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unexpected positioned unknown key error: %v", err)
	}
}

func TestConfigParsePolicyNotAMapping(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().Parse([]byte("policy: true\n"))
	if err == nil {
		t.Fatal("no error occurred")
	}
	var source *repoconfig.SourceError
	if !errors.As(err, &source) || source.Line != 1 || source.Column != 9 || !strings.Contains(err.Error(), "expected a map") {
		t.Fatalf("unexpected positioned mapping error: %v", err)
	}
}

func TestConfigParsePolicyTriState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what  string
		input string
		want  *bool
	}{
		{
			what:  "key is not set",
			input: "policy: {}\n",
			want:  nil,
		},
		{
			what: "key is null",
			input: `policy:
  require-commit-hash:
`,
			want: nil,
		},
		{
			what: "key is false",
			input: `policy:
  require-commit-hash: false
`,
			want: new(false),
		},
		{
			what: "key is true",
			input: `policy:
  require-commit-hash: true
`,
			want: new(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, c.Policy.RequireCommitHash); diff != "" {
				t.Fatal(diff)
			}
			if want := tc.want != nil && *tc.want; c.RequiresCommitHash() != want {
				t.Fatalf("RequiresCommitHash() is %v but wanted %v", c.RequiresCommitHash(), want)
			}
		})
	}
}

func TestConfigPolicyNilConfig(t *testing.T) {
	t.Parallel()
	var c *repoconfig.Config
	if c.RequiresCommitHash() {
		t.Fatal("\"require-commit-hash\" is enabled for a nil config")
	}
}

func TestRemovedRequiredActions(t *testing.T) {
	t.Parallel()
	if _, exists := checking.ConfigurationRules().FindRule("required-actions"); exists {
		t.Fatal("removed rule remains in the catalog")
	}
	for _, input := range []string{
		"policy: {required-actions: [actions/checkout]}",
		"policy: {required-actions: []}",
		"policy: {required-actions: null}",
		"lint: {rules: {policy: {required-actions: on}}}",
		"lint: {rules: {policy: {required-actions: off}}}",
		"lint: {rules: {policy: {required-actions: {level: on, options: {actions: []}}}}}",
	} {
		if _, err := checking.ConfigurationRules().Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "required-actions") {
			t.Fatalf("removed setting accepted or misdiagnosed: %s: %v", input, err)
		}
	}
	for _, input := range []struct{ path, content string }{
		{"config.yaml", "policy: {required-actions: [actions/checkout]}"},
		{"config.yaml", "policy: {required-actions: []}"},
		{"config.toml", "[policy]\nrequired-actions = ['actions/checkout']"},
		{"config.toml", "[policy]\nrequired-actions = []"},
	} {
		if _, err := checking.ConfigurationRules().MigrateDocument(input.path, []byte(input.content)); err == nil || !strings.Contains(err.Error(), "required-actions") {
			t.Fatalf("migration accepted or dropped removed setting: %s: %v", input.content, err)
		}
	}
}

func TestConfigParseRequireJobTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what        string
		input       string
		wantSet     bool
		wantEnabled bool
		wantMax     float64
		wantHasMax  bool
	}{
		{
			what:    "key is not set",
			input:   "policy: {}\n",
			wantSet: false,
		},
		{
			what: "key is null",
			input: `policy:
  require-job-timeout:
`,
			wantSet: false,
		},
		{
			what: "key is false",
			input: `policy:
  require-job-timeout: false
`,
			wantSet:     true,
			wantEnabled: false,
		},
		{
			what: "key is true",
			input: `policy:
  require-job-timeout: true
`,
			wantSet:     true,
			wantEnabled: true,
		},
		{
			what: "key is an empty mapping",
			input: `policy:
  require-job-timeout: {}
`,
			wantSet:     true,
			wantEnabled: true,
		},
		{
			what: "mapping sets max-minutes",
			input: `policy:
  require-job-timeout:
    max-minutes: 60
`,
			wantSet:     true,
			wantEnabled: true,
			wantMax:     60,
			wantHasMax:  true,
		},
		{
			what: "max-minutes is a fraction",
			input: `policy:
  require-job-timeout: {max-minutes: 30.5}
`,
			wantSet:     true,
			wantEnabled: true,
			wantMax:     30.5,
			wantHasMax:  true,
		},
		{
			what: "max-minutes has a leading zero",
			input: `policy:
  require-job-timeout:
    max-minutes: 017
`,
			wantSet:     true,
			wantEnabled: true,
			wantMax:     17,
			wantHasMax:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			c, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			p := c.Policy.RequireJobTimeout
			if tc.wantSet != (p != nil) {
				t.Fatalf("policy is %v but wanted set=%v", p, tc.wantSet)
			}
			if p.Enabled() != tc.wantEnabled {
				t.Fatalf("Enabled() is %v but wanted %v", p.Enabled(), tc.wantEnabled)
			}
			limit, ok := p.MaxMinutes()
			if ok != tc.wantHasMax || limit != tc.wantMax {
				t.Fatalf("MaxMinutes() is (%v, %v) but wanted (%v, %v)", limit, ok, tc.wantMax, tc.wantHasMax)
			}
			if got := c.RequiresJobTimeout(); !cmp.Equal(got, p, cmp.AllowUnexported(repoconfig.JobTimeoutPolicy{})) {
				t.Fatalf("RequiresJobTimeout() is %v but wanted %v", got, p)
			}
		})
	}
}

func TestConfigParseRequireJobTimeoutError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what  string
		input string
		want  string
	}{
		{
			what: "value is a number",
			input: `policy:
  require-job-timeout: 60
`,
			want: `"require-job-timeout" must be a boolean or a mapping at line:2,col:24`,
		},
		{
			what: "value is a string",
			input: `policy:
  require-job-timeout: hello
`,
			want: `"require-job-timeout" must be a boolean or a mapping at line:2,col:24`,
		},
		{
			what: "value is a sequence",
			input: `policy:
  require-job-timeout:
    - 60
`,
			want: `"require-job-timeout" must be a boolean or a mapping at line:3,col:5`,
		},
		{
			what: "unknown key in the mapping",
			input: `policy:
  require-job-timeout:
    max_minutes: 60
`,
			want: `unknown key "max_minutes" in "require-job-timeout" at line:3,col:5`,
		},
		{
			what: "max-minutes is zero",
			input: `policy:
  require-job-timeout:
    max-minutes: 0
`,
			want: `"max-minutes" in "require-job-timeout" must be greater than zero but got 0 at line:3,col:18`,
		},
		{
			what: "max-minutes is negative",
			input: `policy:
  require-job-timeout:
    max-minutes: -5
`,
			want: `"max-minutes" in "require-job-timeout" must be greater than zero but got -5 at line:3,col:18`,
		},
		{
			what: "max-minutes is not a number",
			input: `policy:
  require-job-timeout:
    max-minutes: hello
`,
			want: `"max-minutes" in "require-job-timeout" must be a number but got "hello" at line:3,col:18`,
		},
		{
			what: "max-minutes is a mapping",
			input: `policy:
  require-job-timeout:
    max-minutes:
      a: 1
`,
			want: `"max-minutes" in "require-job-timeout" must be a number but got "" at line:4,col:7`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			_, err := checking.ConfigurationRules().Parse([]byte(tc.input))
			if err == nil {
				t.Fatal("no error occurred")
			}
			var line, column int
			matched := regexp.MustCompile(`line:(\d+),col:(\d+)`).FindStringSubmatch(tc.want)
			if len(matched) != 3 {
				t.Fatalf("missing expected location in %q", tc.want)
			}
			if _, scanErr := fmt.Sscanf(matched[0], "line:%d,col:%d", &line, &column); scanErr != nil {
				t.Fatal(scanErr)
			}
			var source *repoconfig.SourceError
			if !errors.As(err, &source) || source.Line != line || source.Column != column {
				t.Fatalf("source location: %v, want %d:%d", err, line, column)
			}
			message := "must be a boolean or a mapping"
			switch tc.what {
			case "unknown key in the mapping":
				message = "unknown key"
			case "max-minutes is zero", "max-minutes is negative":
				message = "greater than zero"
			case "max-minutes is not a number", "max-minutes is a mapping":
				message = "must be a number"
			}
			if !strings.Contains(err.Error(), message) {
				t.Fatalf("error %q lacks %q", err, message)
			}
		})
	}
}

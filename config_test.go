package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

func TestConfigParseSelfHostedRunnerOK(t *testing.T) {
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
			c, err := ParseConfig([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(c.SelfHostedRunner.Labels, tc.labels); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestConfigParseError(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			in:   `self-hosted-runner: 42`,
			want: `cannot construct`,
		},
		{
			in: `
paths:
  foo:
    ignore: foo+
`,
			want: `"ignore" must be a sequence node`,
		},
		{
			in: `
paths:
  foo:
    ignore: ['(foo']
`,
			want: `invalid regular expression "(foo" in "ignore"`,
		},
		{
			in: `
paths:
  foo.{txt,xml:
`,
			want: `invalid glob pattern`,
		},
		{
			in: `
policy:
  required-actions: true
`,
			want: `"required-actions" must be a sequence node at line:3,col:21`,
		},
		{
			in: `
policy:
  required-actions:
    - 42
`,
			want: `an entry of "required-actions" must be a string at line:4,col:7`,
		},
		{
			in: `
policy:
  required-actions:
    - ""
`,
			want: `an entry of "required-actions" must not be empty at line:4,col:7`,
		},
		{
			in: `
policy:
  required-actions:
    - actions/[checkout
`,
			want: `invalid glob pattern "actions/[checkout" in an entry of "required-actions" at line:4,col:7`,
		},
		{
			in: `
policy:
  required-actions:
    - actions/checkout@[v5
`,
			want: `invalid glob pattern "[v5" in an entry of "required-actions" at line:4,col:7`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.in))
			if err == nil {
				t.Fatal("no error occurred")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted error message %q to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestConfigPathConfigIgnores(t *testing.T) {
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
			var c PathConfig
			if err := yaml.Unmarshal([]byte(tc.input), &c); err != nil {
				t.Fatal(err)
			}
			have := c.Ignore.Match(&Error{Message: tc.msg})
			if tc.want != have {
				t.Fatalf("wanted %v but got %v for message %q and input %q", tc.want, have, tc.msg, tc.input)
			}
		})
	}
}

func TestConfigIgnoreErrors(t *testing.T) {
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

	var cfg Config
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
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
			if c.Ignore.Match(&Error{Message: tc.msg}) {
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
	p := filepath.Join("testdata", "config", "ok.yml")
	c, err := ReadConfigFile(p)
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{"foo", "bar"}
	if diff := cmp.Diff(c.SelfHostedRunner.Labels, labels); diff != "" {
		t.Fatal(diff)
	}
}

func TestConfigReadFileReadError(t *testing.T) {
	p := filepath.Join("testdata", "config", "does-not-exist.yml")
	_, err := ReadConfigFile(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not read config file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigReadFileParseError(t *testing.T) {
	p := filepath.Join("testdata", "config", "broken.yml")
	_, err := ReadConfigFile(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not parse config file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigGenerateDefaultConfigFileOK(t *testing.T) {
	f := filepath.Join(t.TempDir(), "default-config-for-test.yml")
	if err := writeDefaultConfigFile(f); err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfigFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SelfHostedRunner.Labels) != 0 {
		t.Fatal(c.SelfHostedRunner.Labels)
	}
	if c.ConfigVariables != nil {
		t.Fatal(c.SelfHostedRunner.Labels)
	}
	if len(c.Paths) != 0 {
		t.Fatal(c.Paths)
	}
	if diff := cmp.Diff(Policy{}, c.Policy); diff != "" {
		t.Fatal(diff)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "#policy:\n#  # Require every \"uses:\" to be pinned to a full commit SHA or an image\n#  # digest.\n#  require-commit-hash: true\n#  # Actions every workflow must use. \"owner/repo@ref\" also pins the version.\n#  required-actions:\n#    - actions/checkout\n"
	if !strings.Contains(string(b), want) {
		t.Fatalf("wanted generated config file %q to contain %q", string(b), want)
	}
}

func TestConfigGenerateDefaultConfigFileError(t *testing.T) {
	p := filepath.Join("testdata", "config", "dir-does-not-exist", "test.yml")
	err := writeDefaultConfigFile(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not write default configuration file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigParsePolicyEmpty(t *testing.T) {
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
			c, err := ParseConfig([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(Policy{}, c.Policy); diff != "" {
				t.Fatal(diff)
			}
			if c.RequiresCommitHash() {
				t.Fatal("\"require-commit-hash\" is enabled")
			}
		})
	}
}

func TestConfigParsePolicyUnknownKey(t *testing.T) {
	_, err := ParseConfig([]byte("policy:\n  require-commit-hashes: true\n"))
	if err == nil {
		t.Fatal("no error occurred")
	}
	want := `unknown key "require-commit-hashes" in "policy" at line:2,col:3`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("wanted error message %q to contain %q", err.Error(), want)
	}
}

func TestConfigParsePolicyNotAMapping(t *testing.T) {
	_, err := ParseConfig([]byte("policy: true\n"))
	if err == nil {
		t.Fatal("no error occurred")
	}
	want := `"policy" must be a mapping node at line:1,col:9`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("wanted error message %q to contain %q", err.Error(), want)
	}
}

func TestConfigParsePolicyTriState(t *testing.T) {
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
			what:  "key is null",
			input: "policy:\n  require-commit-hash:\n",
			want:  nil,
		},
		{
			what:  "key is false",
			input: "policy:\n  require-commit-hash: false\n",
			want:  new(false),
		},
		{
			what:  "key is true",
			input: "policy:\n  require-commit-hash: true\n",
			want:  new(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			c, err := ParseConfig([]byte(tc.input))
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
	var c *Config
	if c.RequiresCommitHash() {
		t.Fatal("\"require-commit-hash\" is enabled for a nil config")
	}
	if as := c.RequiredActions(); as != nil {
		t.Fatalf("\"required-actions\" is %v for a nil config", as)
	}
}

func TestConfigParseRequiredActionsOK(t *testing.T) {
	tests := []struct {
		what  string
		input string
		want  []string
	}{
		{
			what:  "key is not set",
			input: "policy: {}\n",
			want:  nil,
		},
		{
			what:  "key is null",
			input: "policy:\n  required-actions:\n",
			want:  nil,
		},
		{
			what:  "key is an empty sequence",
			input: "policy:\n  required-actions: []\n",
			want:  []string{},
		},
		{
			what:  "one action without a ref",
			input: "policy:\n  required-actions:\n    - actions/checkout\n",
			want:  []string{"actions/checkout"},
		},
		{
			what:  "one action with a ref",
			input: "policy:\n  required-actions:\n    - my-org/scan@v2\n",
			want:  []string{"my-org/scan@v2"},
		},
		{
			what:  "glob patterns",
			input: "policy:\n  required-actions:\n    - github/codeql-action/*\n    - actions/checkout@v4*\n",
			want:  []string{"github/codeql-action/*", "actions/checkout@v4*"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			c, err := ParseConfig([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, c.Policy.RequiredActions); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(tc.want, c.RequiredActions()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

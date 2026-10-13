package gitrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func renderEntry(key string, value configValue) string {
	if value.bare {
		return key
	}
	return key + "=" + value.text
}

func parseEntries(text string) ([]string, error) {
	var got []string
	err := parseConfig("test", []byte(text), func(section, name string, value configValue) error {
		got = append(got, renderEntry(section+"."+name, value))
		return nil
	})
	return got, err
}

func gitEntries(t *testing.T, dir string, args ...string) ([]string, bool) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"config", "--list", "-z"}, args...)...)
	command.Dir = dir
	out, err := command.Output()
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if entry != "" {
			key, value, hasValue := strings.Cut(entry, "\n")
			entries = append(entries, renderEntry(key, configValue{text: value, bare: !hasValue}))
		}
	}
	return entries, true
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
}

func TestParseConfigMatchesGit(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"sections and values", "[core]\n\tignoreCase = true\n\texcludesFile = ~/.ignore\n", []string{"core.ignorecase=true", "core.excludesfile=~/.ignore"}},
		{"subsections", "[remote \"Origin\"]\n\turl = x\n[branch.Main]\n\tmerge = y\n", []string{"remote.Origin.url=x", "branch.main.merge=y"}},
		{"subsection escapes", "[a \"x\\\"y\\\\z\\qw\"]\n\tb = 1\n", []string{"a.x\"y\\zqw.b=1"}},
		{"empty section name with subsection", "[ \"x\"]\n\ta = 1\n", []string{".x.a=1"}},
		{"comments and blanks", "# top\n; top\n\n[core] # trailing\n\tbare = false ; here\n", []string{"core.bare=false"}},
		{"entry after header", "[core] a = 1\n", []string{"core.a=1"}},
		{"bare key after header", "[core] junk\n", []string{"core.junk"}},
		{"quotes and escapes", "[core]\n\ta = \"x # y\" z\n\tb = \"\\\"q\\\"\"\n\tc = tab\\tbed\\\\\n\td = \\n\\b\n", []string{"core.a=x # y z", "core.b=\"q\"", "core.c=tab\tbed\\", "core.d=\n\b"}},
		{"quoted comment chars", "[core]\n\ta = \"x;y#z\"\n", []string{"core.a=x;y#z"}},
		{"continuation", "[core]\n\ta = one \\\ntwo\n\tb = x\\\r\ny\r\n", []string{"core.a=one two", "core.b=xy"}},
		{"trailing space", "[core]\n\ta = x   \n\tb = \"x  \"\n\tc = x   # c\n", []string{"core.a=x", "core.b=x  ", "core.c=x"}},
		{"leading space after empty quotes", "[core]\n\ta = \"\" x\n", []string{"core.a=x"}},
		{"bare key", "[core]\n\tignorecase\n\tbare\n", []string{"core.ignorecase", "core.bare"}},
		{"bare key at end of file", "[core]\n\ta", []string{"core.a"}},
		{"empty value", "[core]\n\texcludesFile =\n", []string{"core.excludesfile="}},
		{"tabs around equals", "[core]\n\ta\t=\t1\n", []string{"core.a=1"}},
		{"byte order mark", "\xEF\xBB\xBF[core]\n\ta = 1\n", []string{"core.a=1"}},
		{"later wins", "[core]\n\ta = 1\n\ta = 2\n", []string{"core.a=1", "core.a=2"}},
		{"crlf", "[core]\r\n\ta = 1\r\n", []string{"core.a=1"}},
		{"lone carriage return in value", "[core]\n\ta = x\ry\n", []string{"core.a=x\ry"}},
		{"backslash at end of file", "[core]\n\ta = x\\", []string{"core.a=x"}},
		{"value at end of file", "[core]\n\ta = x", []string{"core.a=x"}},
		{"nul in value", "[core]\n\ta = x\x00y\n", []string{"core.a=x"}},
		{"nul in subsection", "[a \"b\x00c\"]\n\tx = 1\n", []string{"a.b=1"}},
		{"header at end of file", "[core]", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEntries(tc.text)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("parseConfig = %q, %v, want %q", got, err, tc.want)
			}
			if theirs, ok := gitParse(t, tc.text); !ok || !slices.Equal(theirs, tc.want) {
				t.Fatalf("git config = %q, %v, want %q", theirs, ok, tc.want)
			}
		})
	}
}

func TestParseConfigRejectsWhatGitRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"unterminated header", "[core\n\ta = 1\n"},
		{"space before bracket", "[core ]\n\ta = 1\n"},
		{"subsection without space", "[core\"x\"]\n\ta = 1\n"},
		{"space after subsection", "[core \"x\" ]\n\ta = 1\n"},
		{"empty header", "[]\n\ta = 1\n"},
		{"newline in subsection", "[core \"a\\\nb\"]\n"},
		{"unknown escape", "[core]\n\ta = x\\qy\n"},
		{"unterminated quote", "[core]\n\ta = \"x\n"},
		{"line starts with equals", "[core]\n\t= 2\n"},
		{"line starts with digit", "[core]\n\t1a = 1\n"},
		{"bare key with comment", "[core]\n\ta # c\n"},
		{"bare key with semicolon", "[core]\n\ta ; c\n"},
		{"carriage return before equals", "[core]\n\ta\r= 1\n"},
		{"vertical tab", "[core]\n\va = 1\n"},
		{"partial byte order mark", "\xEF\xBB[core]\n\ta = 1\n"},
		{"dot in name", "[core]\n\ta.b = 1\n"},
		{"underscore in name", "[core]\n\ta_b = 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := parseEntries(tc.text); err == nil {
				t.Fatalf("parseConfig = %q, want an error", got)
			}
			if theirs, ok := gitParse(t, tc.text); ok {
				t.Fatalf("git config = %q, want an error", theirs)
			}
		})
	}
}

func gitParse(t *testing.T, text string) ([]string, bool) {
	t.Helper()
	requireGit(t)
	file := filepath.Join(t.TempDir(), "config")
	writeConfig(t, file, text)
	entries, ok := gitEntries(t, filepath.Dir(file), "--file", file)
	if entries == nil {
		entries = []string{}
	}
	return entries, ok
}

func (r *testRepo) settings() config {
	r.t.Helper()
	repo := r.open()
	settings, err := repo.config(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	return settings
}

func lastText(settings config, key string) string {
	value, _ := settings.last(key)
	return value.text
}

func TestParseConfigSkipsKeysOutsideSections(t *testing.T) {
	got, err := parseEntries("a = 1\n[core]\n\tb = 2\n")
	if err != nil || !slices.Equal(got, []string{"core.b=2"}) {
		t.Fatalf("parseConfig = %q, %v", got, err)
	}
}

func TestParseBoolMatchesGit(t *testing.T) {
	requireGit(t)
	values := []string{"", "true", "TRUE", "yEs", "on", "false", "NO", "off", "1", "0", "2", "-1", "1k", "0k", "1K", "1m", "1g", "2g", "0x10", "0X0", "010", "08", " 1", "\t1", "\v1", "1 ", "+1", "-0", "-", "0x", "2147483647", "2147483648", "-2147483648", "-2147483649", "99999999999999999999", "1kb", "1.0", "maybe", "yesno"}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			ours, err := parseBool(value)
			command := exec.CommandContext(t.Context(), "git", "-c", "core.x="+value, "config", "--type=bool", "core.x")
			command.Dir = t.TempDir()
			command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
			out, gitErr := command.Output()
			if (err == nil) != (gitErr == nil) {
				t.Fatalf("parseBool(%q) error %v, git error %v", value, err, gitErr)
			}
			if err == nil && strings.TrimSpace(string(out)) != map[bool]string{true: "true", false: "false"}[ours] {
				t.Fatalf("parseBool(%q) = %v, git says %s", value, ours, out)
			}
		})
	}
}

func TestConfigBoolean(t *testing.T) {
	settings := config{}
	if got, err := settings.boolean("core.missing"); err != nil || got {
		t.Fatalf("boolean(missing) = %v, %v", got, err)
	}
	settings.add("core", "flag", configValue{bare: true})
	if got, err := settings.boolean("core.flag"); err != nil || !got {
		t.Fatalf("boolean(bare) = %v, %v", got, err)
	}
	settings.add("core", "bad", valueOf("maybe"))
	settings.add("core", "bad", valueOf("false"))
	if got, err := settings.boolean("core.bad"); err == nil {
		t.Fatalf("boolean with an earlier bad value = %v, want an error", got)
	}
}

func clearCommandConfig(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_KEY_1", "GIT_CONFIG_VALUE_1"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommandConfigMatchesGit(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"unset", nil, []string{}},
		{"count", map[string]string{"GIT_CONFIG_COUNT": "2", "GIT_CONFIG_KEY_0": "Core.X", "GIT_CONFIG_VALUE_0": "a", "GIT_CONFIG_KEY_1": "Remote.Up.Stream.URL", "GIT_CONFIG_VALUE_1": ""}, []string{"core.x=a", "remote.Up.Stream.url="}},
		{"empty count", map[string]string{"GIT_CONFIG_COUNT": ""}, []string{}},
		{"count with leading space", map[string]string{"GIT_CONFIG_COUNT": " 1", "GIT_CONFIG_KEY_0": "core.x", "GIT_CONFIG_VALUE_0": "a"}, []string{"core.x=a"}},
		{"count with plus", map[string]string{"GIT_CONFIG_COUNT": "+1", "GIT_CONFIG_KEY_0": "core.x", "GIT_CONFIG_VALUE_0": "a"}, []string{"core.x=a"}},
		{"negative zero count", map[string]string{"GIT_CONFIG_COUNT": "-0"}, []string{}},
		{"section starting with digit", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "1a.x", "GIT_CONFIG_VALUE_0": "a"}, []string{"1a.x=a"}},
		{"negative count", map[string]string{"GIT_CONFIG_COUNT": "-1"}, nil},
		{"huge count", map[string]string{"GIT_CONFIG_COUNT": "99999999999999999999"}, nil},
		{"count with trailing space", map[string]string{"GIT_CONFIG_COUNT": "1 ", "GIT_CONFIG_KEY_0": "core.x", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"blank count", map[string]string{"GIT_CONFIG_COUNT": " "}, nil},
		{"hex count", map[string]string{"GIT_CONFIG_COUNT": "0x1"}, nil},
		{"missing key", map[string]string{"GIT_CONFIG_COUNT": "1"}, nil},
		{"missing value", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.x"}, nil},
		{"empty key", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"invalid name", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.1x", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"non-ascii name", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.xé", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"no section", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "x", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"no name", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"newline in subsection", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "a.b\nc.x", "GIT_CONFIG_VALUE_0": "a"}, nil},
		{"parameters", map[string]string{"GIT_CONFIG_PARAMETERS": "'core.x'='a b' 'core.y'= 'core.Z'\t'core.w=v=w' 'core.v'  "}, []string{"core.x=a b", "core.y", "core.z", "core.w=v=w", "core.v"}},
		{"escaped quote and bang", map[string]string{"GIT_CONFIG_PARAMETERS": `'core.x'='it'\''s'\!'!'`}, []string{"core.x=it's!!"}},
		{"empty parameters", map[string]string{"GIT_CONFIG_PARAMETERS": ""}, []string{}},
		{"count before parameters", map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.x", "GIT_CONFIG_VALUE_0": "count", "GIT_CONFIG_PARAMETERS": "'core.x'='parameters'"}, []string{"core.x=count", "core.x=parameters"}},
		{"bad escape", map[string]string{"GIT_CONFIG_PARAMETERS": `'core.x'='a'\b'c'`}, nil},
		{"leading space", map[string]string{"GIT_CONFIG_PARAMETERS": " 'core.x'"}, nil},
		{"unquoted key", map[string]string{"GIT_CONFIG_PARAMETERS": "core.x"}, nil},
		{"unquoted value", map[string]string{"GIT_CONFIG_PARAMETERS": "'core.x'=a"}, nil},
		{"junk after value", map[string]string{"GIT_CONFIG_PARAMETERS": "'core.x'='a'b"}, nil},
		{"junk after key", map[string]string{"GIT_CONFIG_PARAMETERS": "'core.x'b"}, nil},
		{"unterminated", map[string]string{"GIT_CONFIG_PARAMETERS": "'core.x"}, nil},
		{"empty old style key", map[string]string{"GIT_CONFIG_PARAMETERS": "'=a'"}, nil},
		{"invalid parameter key", map[string]string{"GIT_CONFIG_PARAMETERS": "'core'='a'"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearCommandConfig(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			params, err := commandConfig()
			got := []string{}
			for _, param := range params {
				got = append(got, renderEntry(param.section+"."+param.name, param.value))
			}
			if (err == nil) != (tc.want != nil) || (err == nil && !slices.Equal(got, tc.want)) {
				t.Fatalf("commandConfig = %q, %v, want %q", got, err, tc.want)
			}
			if _, err := exec.LookPath("git"); err != nil {
				return
			}
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
			theirs, ok := gitEntries(t, t.TempDir())
			if ok != (tc.want != nil) || (ok && len(theirs)+len(tc.want) > 0 && !slices.Equal(theirs, tc.want)) {
				t.Fatalf("git config = %q, %v, want %q", theirs, ok, tc.want)
			}
		})
	}
}

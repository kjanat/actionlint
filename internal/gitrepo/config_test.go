package gitrepo

import (
	"maps"
	"testing"
)

func parseInto(t *testing.T, text string) config {
	t.Helper()
	got := config{}
	err := parseConfig([]byte(text), func(section, name, value string) error {
		got[section+"."+name] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want config
	}{
		{"sections and values", "[core]\n\tignoreCase = true\n\texcludesFile = ~/.ignore\n", config{"core.ignorecase": "true", "core.excludesfile": "~/.ignore"}},
		{"subsections", "[remote \"origin\"]\n\turl = x\n[branch.Main]\n\tmerge = y\n", config{"remote.origin.url": "x", "branch.main.merge": "y"}},
		{"comments and blanks", "# top\n; top\n\n[core] # trailing\n\tbare = false ; here\n", config{"core.bare": "false"}},
		{"quotes and escapes", "[core]\n\ta = \"x # y\" z\n\tb = \"\\\"q\\\"\"\n\tc = tab\\tbed\\\\\n", config{"core.a": "x # y z", "core.b": "\"q\"", "core.c": "tab\tbed\\"}},
		{"continuation", "[core]\n\ta = one \\\ntwo\n", config{"core.a": "one two"}},
		{"trailing space", "[core]\n\ta = x   \n\tb = \"x  \"\n", config{"core.a": "x", "core.b": "x  "}},
		{"bare key", "[core]\n\tignorecase\n\tbare\n", config{"core.ignorecase": "true", "core.bare": "true"}},
		{"empty value", "[core]\n\texcludesFile =\n", config{"core.excludesfile": ""}},
		{"byte order mark", "\xEF\xBB\xBF[core]\n\ta = 1\n", config{"core.a": "1"}},
		{"later wins", "[core]\n\ta = 1\n\ta = 2\n", config{"core.a": "2"}},
		{"garbage", "[core\n\ta = 1\n[core]\n\t= 2\n\tb = 3\n", config{"core.b": "3"}},
		{"crlf", "[core]\r\n\ta = 1\r\n", config{"core.a": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseInto(t, tc.text); !maps.Equal(got, tc.want) {
				t.Fatalf("parseConfig = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "YES": true, "on": true, "1": true, "2": true, "-1": true, "false": false, "no": false, "Off": false, "0": false, "": false} {
		got, err := parseBool(value)
		if err != nil || got != want {
			t.Errorf("parseBool(%q) = %v, %v, want %v", value, got, err, want)
		}
	}
	if got, err := parseBool("maybe"); err == nil {
		t.Fatalf("parseBool(maybe) = %v, want an error", got)
	}
	if got, err := (config{}).boolean("missing"); err != nil || got {
		t.Fatalf("boolean(missing) = %v, %v", got, err)
	}
}

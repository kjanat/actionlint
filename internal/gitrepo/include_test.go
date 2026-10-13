package gitrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigIncludes(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("checkout", "--quiet", "-b", "feature/x")
	fixture.git("config", "remote.origin.url", "https://github.com/kjanat/actionlint")
	home := fixture.home
	writeConfig(t, filepath.Join(home, "plain"), "[core]\n\tplain = 1\n")
	writeConfig(t, filepath.Join(home, "nested", "deeper"), "[core]\n\tdeeper = 1\n")
	writeConfig(t, filepath.Join(home, "nested", "mid"), "[include]\n\tpath = deeper\n")
	writeConfig(t, filepath.Join(home, "matched"), "[core]\n\tmatched = 1\n")
	writeConfig(t, filepath.Join(home, "unmatched"), "[core]\n\tunmatched = 1\n")
	root := filepath.Join(home, "gitconfig")
	text := strings.Join([]string{
		"[include]",
		"\tpath = ~/plain",
		"\tpath = nested/mid",
		"\tpath = missing",
		"[includeIf \"gitdir:" + filepath.ToSlash(fixture.root) + "/\"]",
		"\tpath = matched",
		"[includeIf \"gitdir:/elsewhere/\"]",
		"\tpath = unmatched",
		"[includeIf \"gitdir/i:" + strings.ToUpper(filepath.ToSlash(fixture.root)) + "/\"]",
		"\tpath = matched",
		"[includeIf \"gitdir:repo/\"]",
		"\tpath = matched",
		"[includeIf \"gitdir:./repo/\"]",
		"\tpath = matched",
		"[includeIf \"onbranch:feature/**\"]",
		"\tpath = matched",
		"[includeIf \"onbranch:main\"]",
		"\tpath = unmatched",
		"[includeIf \"hasconfig:remote.*.url:https://github.com/kjanat/**\"]",
		"\tpath = matched",
		"[includeIf \"hasconfig:remote.*.url:https://gitlab.com/**\"]",
		"\tpath = unmatched",
		"",
	}, "\n")
	writeConfig(t, root, text)
	t.Setenv("GIT_CONFIG_GLOBAL", root)
	settings, err := fixture.open().config()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"core.plain", "core.deeper", "core.matched"} {
		if settings[key] != "1" {
			t.Errorf("%s = %q, want 1", key, settings[key])
		}
	}
	if _, ok := settings["core.unmatched"]; ok {
		t.Error("an unmatched includeIf was followed")
	}
	want := strings.TrimSpace(fixture.git("config", "--get-all", "core.matched"))
	if strings.Count(want, "1") != 5 {
		t.Fatalf("git sees %q for core.matched, want five values", want)
	}
}

func TestLoadConfigIncludeDepth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loop")
	writeConfig(t, path, "[include]\n\tpath = loop\n")
	ctx, err := newIncludeContext("")
	if err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(path, config{}, ctx, 0); err == nil {
		t.Fatal("a self-including config loaded")
	}
}

func TestLoadConfigRejectsUnknownCondition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeConfig(t, path, "[includeIf \"hasconfig:core.bare:true\"]\n\tpath = x\n")
	ctx, err := newIncludeContext("")
	if err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(path, config{}, ctx, 0); err == nil {
		t.Fatal("an unsupported condition loaded")
	}
}

func TestWildmatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		fold, want    bool
	}{
		{"/home/x/**", "/home/x/.git", false, true},
		{"/home/x/**", "/home/y/.git", false, false},
		{"**/work/**", "/home/x/work/a/.git", false, true},
		{"**/work/**", "/home/x/worker/a/.git", false, false},
		{"**/.git", "/home/x/.git", false, true},
		{"/HOME/x/**", "/home/x/.git", true, true},
		{"/HOME/x/**", "/home/x/.git", false, false},
		{"feature/**", "feature/x", false, true},
		{"feature/**", "feature", false, false},
		{"main", "main", false, true},
		{"https://github.com/kjanat/**", "https://github.com/kjanat/actionlint", false, true},
		{"*.git", "/home/x/.git", false, false},
	} {
		if got := wildmatch(tc.pattern, tc.text, tc.fold); got != tc.want {
			t.Errorf("wildmatch(%q, %q, %v) = %v, want %v", tc.pattern, tc.text, tc.fold, got, tc.want)
		}
	}
}

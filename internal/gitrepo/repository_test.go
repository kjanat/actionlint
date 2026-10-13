package gitrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestOpenOutsideRepository(t *testing.T) {
	repo, err := Open(t.TempDir())
	if err != nil || repo != nil {
		t.Fatalf("Open = %v, %v, want nil, nil", repo, err)
	}
}

func TestOpenFindsAncestor(t *testing.T) {
	fixture := newTestRepo(t)
	nested := fixture.write("a/b/file", "")
	repo, err := Open(filepath.Dir(nested))
	if err != nil {
		t.Fatal(err)
	}
	if repo.WorkTree != fixture.root || repo.GitDir != filepath.Join(fixture.root, ".git") || repo.CommonDir != repo.GitDir {
		t.Fatalf("Open = %+v", repo)
	}
}

func TestOpenWorktree(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	fixture.git("commit", "--quiet", "-m", "Fixture")
	worktree := filepath.Join(t.TempDir(), "wt")
	fixture.git("worktree", "add", "--quiet", worktree)
	repo, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if repo.WorkTree != worktree {
		t.Fatalf("WorkTree = %q, want %q", repo.WorkTree, worktree)
	}
	mainGitDir, err := filepath.EvalSymlinks(filepath.Join(fixture.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(repo.GitDir, filepath.Join(mainGitDir, "worktrees")) {
		t.Fatalf("GitDir = %q", repo.GitDir)
	}
	if repo.CommonDir != mainGitDir {
		t.Fatalf("CommonDir = %q, want %q", repo.CommonDir, mainGitDir)
	}
}

func TestOpenRejectsBrokenGitFile(t *testing.T) {
	for name, content := range map[string]string{"no gitdir line": "nonsense\n", "missing target": "gitdir: elsewhere\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ".git"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if repo, err := Open(root); err == nil {
				t.Fatalf("Open = %+v, want an error", repo)
			}
		})
	}
}

func TestOpenReadsConfig(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "core.ignoreCase", "true")
	fixture.git("config", "core.precomposeUnicode", "true")
	fixture.git("config", "core.excludesFile", "~/global-ignore")
	repo := fixture.open()
	if !repo.IgnoreCase() {
		t.Fatal("IgnoreCase = false")
	}
	if !repo.PrecomposeUnicode() {
		t.Fatal("PrecomposeUnicode = false")
	}
	if want := filepath.Join(fixture.home, "global-ignore"); repo.excludes != want {
		t.Fatalf("excludes = %q, want %q", repo.excludes, want)
	}
	if repo.hashLength != 20 {
		t.Fatalf("hashLength = %d", repo.hashLength)
	}
}

func TestOpenExcludesFileSettings(t *testing.T) {
	for name, tc := range map[string]struct{ value, want string }{
		"empty disables": {"", ""},
		"relative":       {"mine", "mine"},
		"nested":         {"config/ignore", filepath.Join("config", "ignore")},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.git("config", "core.excludesFile", tc.value)
			want := tc.want
			if want != "" {
				want = filepath.Join(fixture.root, want)
			}
			if repo := fixture.open(); repo.excludes != want {
				t.Fatalf("excludes = %q, want %q", repo.excludes, want)
			}
		})
	}
}

func TestOpenAbsoluteCommonDir(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	fixture.git("commit", "--quiet", "-m", "Fixture")
	worktree := filepath.Join(t.TempDir(), "wt")
	fixture.git("worktree", "add", "--quiet", worktree)
	linked, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked.GitDir, "commondir"), []byte(linked.CommonDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if again.CommonDir != linked.CommonDir {
		t.Fatalf("CommonDir = %q, want %q", again.CommonDir, linked.CommonDir)
	}
}

func TestOpenSymlinkedGitDir(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	elsewhere := filepath.Join(t.TempDir(), "metadata")
	if err := os.Rename(filepath.Join(fixture.root, ".git"), elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(fixture.root, ".git")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fixture.git("status", "--short")
	repo := fixture.open()
	if repo.GitDir != filepath.Join(fixture.root, ".git") {
		t.Fatalf("GitDir = %q", repo.GitDir)
	}
	if got, err := repo.TrackedFiles(); err != nil || len(got) != 1 {
		t.Fatalf("TrackedFiles = %q, %v", got, err)
	}
}

func TestOpenDefaultExcludesFile(t *testing.T) {
	fixture := newTestRepo(t)
	repo := fixture.open()
	if want := filepath.Join(fixture.home, ".config", "git", "ignore"); repo.excludes != want {
		t.Fatalf("excludes = %q, want %q", repo.excludes, want)
	}
}

func TestOpenGlobalConfigFile(t *testing.T) {
	fixture := newTestRepo(t)
	global := filepath.Join(fixture.home, "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\n\texcludesFile = /elsewhere/ignore\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if repo := fixture.open(); repo.excludes != filepath.FromSlash("/elsewhere/ignore") {
		t.Fatalf("excludes = %q", repo.excludes)
	}
}

func TestOpenWithoutHome(t *testing.T) {
	for name, home := range map[string]string{"unset": "", "dev null": os.DevNull} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write(".gitignore", "ignored/\n")
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if home == "" {
				for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
					if err := os.Unsetenv(name); err != nil {
						t.Fatal(err)
					}
				}
			}
			repo := fixture.open()
			ignores, err := repo.Ignores()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := ignores.Ignored("ignored", true); err != nil || !got {
				t.Fatalf("Ignored(ignored) = %v, %v", got, err)
			}
			if _, err := repo.TrackedFiles(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenSha256(t *testing.T) {
	fixture := newTestRepo(t, "--object-format=sha256")
	if repo := fixture.open(); repo.hashLength != 32 {
		t.Fatalf("hashLength = %d, want 32", repo.hashLength)
	}
}

func (r *testRepo) gitExit(args ...string) int {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	r.t.Fatalf("git %v: %v", args, err)
	return -1
}

func (r *testRepo) probe(key string) int {
	r.t.Helper()
	repo, err := Open(r.root)
	if err != nil {
		return 128
	}
	format, err := readRepositoryFormat(filepath.Join(repo.CommonDir, "config"))
	if err != nil {
		return 128
	}
	settings, err := repo.config(format)
	switch _, ok := settings.last(key); {
	case err != nil:
		return 128
	case ok:
		return 0
	}
	return 1
}

func (r *testRepo) compareProbe(key string) {
	r.t.Helper()
	ours, theirs := r.probe(key), r.gitExit("config", "--get", key)
	if ours != theirs {
		r.t.Fatalf("%s: ours %d, git %d", key, ours, theirs)
	}
}

func TestOpenConfigChainOrderMatchesGit(t *testing.T) {
	fixture := newTestRepo(t)
	system := filepath.Join(fixture.home, "system")
	writeConfig(t, system, "[core]\n\torder = system\n")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	t.Setenv("GIT_CONFIG_SYSTEM", system)
	writeConfig(t, filepath.Join(fixture.home, ".config", "git", "config"), "[core]\n\torder = xdg\n")
	writeConfig(t, filepath.Join(fixture.home, ".gitconfig"), "[core]\n\torder = home\n")
	fixture.git("config", "core.repositoryFormatVersion", "1")
	fixture.git("config", "extensions.worktreeConfig", "true")
	fixture.git("config", "--add", "core.order", "repository")
	fixture.git("config", "--worktree", "core.order", "worktree")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.order")
	t.Setenv("GIT_CONFIG_VALUE_0", "count")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.order'='parameters'")
	var ours []string
	for _, value := range fixture.settings()["core.order"] {
		ours = append(ours, value.text)
	}
	theirs := strings.Fields(fixture.git("config", "--get-all", "core.order"))
	want := []string{"system", "xdg", "home", "repository", "worktree", "count", "parameters"}
	if !slices.Equal(ours, want) || !slices.Equal(theirs, want) {
		t.Fatalf("order: ours %q, git %q, want %q", ours, theirs, want)
	}
}

func TestOpenEmptyConfigEnvironmentMatchesGit(t *testing.T) {
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			writeConfig(t, filepath.Join(fixture.home, ".gitconfig"), "[core]\n\tprobe = 1\n")
			writeConfig(t, filepath.Join(fixture.home, ".config", "git", "config"), "[core]\n\tprobe = 1\n")
			if name == "GIT_CONFIG_SYSTEM" {
				t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
				t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(fixture.home, ".gitconfig"))
				t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			}
			fixture.compareProbe("core.probe")
			if got := fixture.probe("core.probe"); got != 0 {
				t.Fatalf("probe = %d, want 0", got)
			}
			t.Setenv(name, "")
			fixture.compareProbe("core.probe")
			if got := fixture.probe("core.probe"); got != 1 {
				t.Fatalf("probe = %d, want 1", got)
			}
		})
	}
}

func TestOpenSkipsUnusableConfigFilesLikeGit(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		fixture := newTestRepo(t)
		fixture.git("config", "core.probe", "1")
		if err := os.MkdirAll(filepath.Join(fixture.home, ".config", "git", "config"), 0750); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
		t.Setenv("GIT_CONFIG_SYSTEM", fixture.home)
		fixture.compareProbe("core.probe")
		if got := fixture.probe("core.probe"); got != 0 {
			t.Fatalf("probe = %d, want 0", got)
		}
	})
	t.Run("unreadable global", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs file permissions that bind the current user")
		}
		fixture := newTestRepo(t)
		global := filepath.Join(fixture.home, ".gitconfig")
		writeConfig(t, global, "[core]\n\tprobe = 1\n")
		if err := os.Chmod(global, 0); err != nil {
			t.Fatal(err)
		}
		fixture.git("config", "core.other", "1")
		fixture.compareProbe("core.other")
		fixture.compareProbe("core.probe")
	})
	t.Run("bad GIT_CONFIG_NOSYSTEM", func(t *testing.T) {
		fixture := newTestRepo(t)
		t.Setenv("GIT_CONFIG_NOSYSTEM", "maybe")
		fixture.compareProbe("core.bare")
	})
}

func TestOpenRelativeXDGConfigHomeMatchesGit(t *testing.T) {
	fixture := newTestRepo(t)
	writeConfig(t, filepath.Join(fixture.root, "xdg", "git", "config"), "[core]\n\tprobe = 1\n")
	t.Chdir(fixture.root)
	t.Setenv("XDG_CONFIG_HOME", "xdg")
	fixture.compareProbe("core.probe")
	if got := fixture.probe("core.probe"); got != 0 {
		t.Fatalf("probe = %d, want 0", got)
	}
}

func TestOpenCommandConfigIncludesMatchGit(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"absolute include":   {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"home include":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "~/inc"},
		"relative include":   {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "inc"},
		"bare include":       {"GIT_CONFIG_PARAMETERS": "'include.path'"},
		"dot gitdir":         {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:./elsewhere/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"matching gitdir":    {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:repo/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"other gitdir":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:other/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"bare false include": {"GIT_CONFIG_PARAMETERS": "'includeIf.onbranch:nope.path'"},
		"remote url":         {"GIT_CONFIG_PARAMETERS": "'remote.origin.url'='https://example.com/x' 'includeIf.hasconfig:remote.*.url:https://example.com/**.path'='{home}/inc'"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			writeConfig(t, filepath.Join(fixture.home, "inc"), "[core]\n\tinc = 1\n")
			for key, value := range env {
				t.Setenv(key, strings.ReplaceAll(value, "{home}", filepath.ToSlash(fixture.home)))
			}
			fixture.compareProbe("core.inc")
		})
	}
}

func TestOpenRepositoryFormatMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		global, setup string
	}{
		"global object format":      {global: "[extensions]\n\tobjectFormat = sha256\n"},
		"global worktree config":    {global: "[extensions]\n\tworktreeConfig = true\n", setup: "worktree"},
		"version absent":            {setup: "unversioned"},
		"upper case object format":  {setup: "upper"},
		"bare object format":        {setup: "bare"},
		"bad version":               {setup: "badversion"},
		"versioned worktree config": {setup: "versioned"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write("file", "")
			fixture.git("add", "file")
			if tc.global != "" {
				global := filepath.Join(fixture.home, "gitconfig")
				writeConfig(t, global, tc.global)
				t.Setenv("GIT_CONFIG_GLOBAL", global)
			}
			config := filepath.Join(fixture.root, ".git", "config")
			worktree := filepath.Join(fixture.root, ".git", "config.worktree")
			switch tc.setup {
			case "worktree":
				writeConfig(t, worktree, "[core]\n\tprobe = 1\n")
			case "versioned":
				writeConfig(t, worktree, "[core]\n\tprobe = 1\n")
				fixture.git("config", "core.repositoryFormatVersion", "+1")
				fixture.git("config", "extensions.worktreeConfig", "yes")
			case "unversioned":
				fixture.git("config", "--unset", "core.repositoryFormatVersion")
				fixture.git("config", "extensions.objectFormat", "sha256")
			case "upper":
				fixture.git("config", "core.repositoryFormatVersion", "1")
				fixture.git("config", "extensions.objectFormat", "SHA256")
			case "bare":
				fixture.git("config", "core.repositoryFormatVersion", "1")
				data, err := os.ReadFile(config)
				if err != nil {
					t.Fatal(err)
				}
				writeConfig(t, config, string(data)+"[extensions]\n\tobjectFormat\n")
			case "badversion":
				fixture.git("config", "core.repositoryFormatVersion", "one")
			}
			command := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-object-format")
			command.Dir = fixture.root
			out, gitErr := command.Output()
			repo, err := Open(fixture.root)
			if (err == nil) != (gitErr == nil) {
				t.Fatalf("Open error %v, git error %v", err, gitErr)
			}
			if err != nil {
				return
			}
			want := map[string]int{"sha1": 20, "sha256": 32}[strings.TrimSpace(string(out))]
			if repo.hashLength != want {
				t.Fatalf("hashLength = %d, git says %s", repo.hashLength, out)
			}
			if got, err := repo.TrackedFiles(); err != nil || !slices.Equal(got, []string{"file"}) {
				t.Fatalf("TrackedFiles = %q, %v", got, err)
			}
			fixture.compareProbe("core.probe")
		})
	}
}

func TestOpenExcludesFileMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		global, local string
	}{
		"bare":                   {local: "[core]\n\texcludesFile\n"},
		"bare overridden":        {global: "[core]\n\texcludesFile\n", local: "[core]\n\texcludesFile = mine\n"},
		"optional missing":       {local: "[core]\n\texcludesFile = :(optional)missing\n"},
		"optional present":       {local: "[core]\n\texcludesFile = :(optional)mine\n"},
		"optional after present": {global: "[core]\n\texcludesFile = mine\n", local: "[core]\n\texcludesFile = :(optional)missing\n"},
		"unknown user":           {local: "[core]\n\texcludesFile = ~nobody-gitrepo-test/x\n"},
		"home":                   {local: "[core]\n\texcludesFile = ~/home-ignore\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write("mine", "from-mine\n")
			writeConfig(t, filepath.Join(fixture.home, ".config", "git", "ignore"), "from-xdg\n")
			writeConfig(t, filepath.Join(fixture.home, "home-ignore"), "from-home\n")
			global := filepath.Join(fixture.home, "gitconfig")
			writeConfig(t, global, tc.global)
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			data, err := os.ReadFile(filepath.Join(fixture.root, ".git", "config"))
			if err != nil {
				t.Fatal(err)
			}
			writeConfig(t, filepath.Join(fixture.root, ".git", "config"), string(data)+tc.local)
			for _, file := range []string{"from-mine", "from-xdg", "from-home"} {
				theirs := fixture.gitExit("check-ignore", "--quiet", "--no-index", file)
				ours := 128
				if repo, err := Open(fixture.root); err == nil {
					ignores, err := repo.Ignores()
					if err != nil {
						t.Fatal(err)
					}
					ignored, err := ignores.Ignored(file, false)
					if err != nil {
						t.Fatal(err)
					}
					ours = map[bool]int{true: 0, false: 1}[ignored]
				}
				if ours != theirs {
					t.Fatalf("%s: ours %d, git %d", file, ours, theirs)
				}
			}
		})
	}
}

func TestLookupHome(t *testing.T) {
	dir := t.TempDir()
	drive := filepath.VolumeName(dir)
	path := dir[len(drive):]
	for _, tc := range []struct {
		name    string
		windows bool
		env     map[string]string
		want    string
		ok      bool
	}{
		{"home", false, map[string]string{"HOME": "/h", "USERPROFILE": "/u"}, "/h", true},
		{"empty home", false, map[string]string{"HOME": ""}, "", true},
		{"unset on unix", false, map[string]string{"USERPROFILE": "/u"}, "", false},
		{"home on windows", true, map[string]string{"HOME": "/h", "USERPROFILE": "/u"}, "/h", true},
		{"home drive and path", true, map[string]string{"HOMEDRIVE": drive, "HOMEPATH": path, "USERPROFILE": "/u"}, dir, true},
		{"missing home path", true, map[string]string{"HOMEDRIVE": drive, "HOMEPATH": path + "-missing", "USERPROFILE": "/u"}, "/u", true},
		{"drive without path", true, map[string]string{"HOMEDRIVE": drive, "USERPROFILE": "/u"}, "/u", true},
		{"nothing on windows", true, nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lookupHome(tc.windows, func(name string) (string, bool) {
				value, ok := tc.env[name]
				return value, ok
			})
			if got != tc.want || ok != tc.ok {
				t.Fatalf("lookupHome = %q, %v, want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ path, want string }{
		{"%(prefix)/etc/x", gitPrefix + "/etc/x"},
		{"%(prefix)//abs", "/abs"},
		{"~", filepath.ToSlash(home)},
		{"~/x", filepath.ToSlash(home) + "/x"},
		{"x/~", "x/~"},
	} {
		if got, ok := expandHome(tc.path, false); !ok || got != tc.want {
			t.Errorf("expandHome(%q) = %q, %v, want %q", tc.path, got, ok, tc.want)
		}
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, ok := xdgConfigHome(); !ok || got != "/.config" {
		t.Fatalf("xdgConfigHome with empty HOME = %q, %v", got, ok)
	}
}

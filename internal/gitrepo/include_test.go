package gitrepo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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
	root := filepath.ToSlash(longPath(fixture.root))
	physical := filepath.ToSlash(longPath(resolvedPath(t, fixture.root)))
	conditions := map[string]string{
		"plain":            "",
		"nested":           "",
		"gitdir":           "gitdir:" + root + "/",
		"gitdirOther":      "gitdir:/elsewhere/",
		"gitdirFold":       "gitdir/i:" + strings.ToUpper(root) + "/",
		"gitdirRelative":   "gitdir:repo/",
		"gitdirDot":        "gitdir:./repo/",
		"worktree":         "worktree:" + physical,
		"worktreeOther":    "worktree:" + physical + "/sub",
		"branch":           "onbranch:feature/**",
		"branchOther":      "onbranch:main",
		"remote":           "hasconfig:remote.*.url:https://github.com/kjanat/**",
		"remoteOther":      "hasconfig:remote.*.url:https://gitlab.com/**",
		"unknownCondition": "unknown:x",
	}
	var text strings.Builder
	for key, condition := range conditions {
		target := key
		if key == "nested" {
			writeConfig(t, filepath.Join(home, "nested", "deeper"), "[core]\n\tnested = 1\n")
			writeConfig(t, filepath.Join(home, "nested", "mid"), "[include]\n\tpath = deeper\n")
			target = "nested/mid"
		} else {
			writeConfig(t, filepath.Join(home, key), "[core]\n\t"+key+" = 1\n")
		}
		if condition == "" {
			text.WriteString("[include]\n")
		} else {
			fmt.Fprintf(&text, "[includeIf %q]\n", condition)
		}
		fmt.Fprintf(&text, "\tpath = %s\n", target)
	}
	text.WriteString("[include]\n\tpath = missing\n\tpath = ~/plain\n")
	writeConfig(t, filepath.Join(home, "worktreeProbe"), "[core]\n\tworktreeProbe = 1\n")
	text.WriteString("[includeIf \"worktree:**\"]\n\tpath = worktreeProbe\n")
	global := filepath.Join(home, "gitconfig")
	writeConfig(t, global, text.String())
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	settings := fixture.settings()
	gitKnowsWorktree := fixture.gitBool("config", "--get", "core.worktreeProbe")
	for key := range conditions {
		if strings.HasPrefix(conditions[key], "worktree:") && !gitKnowsWorktree {
			continue
		}
		_, ours := settings["core."+strings.ToLower(key)]
		theirs := fixture.gitBool("config", "--get", "core."+key)
		if ours != theirs {
			t.Errorf("%s (%q): included by us %v, by git %v", key, conditions[key], ours, theirs)
		}
	}
	if _, ok := settings["core.worktreeprobe"]; !ok {
		t.Error("worktree:** did not match")
	}
	for key, want := range map[string]bool{"plain": true, "nested": true, "gitdir": true, "gitdirOther": false, "worktree": true, "worktreeOther": false, "branch": true, "branchOther": false, "remote": true, "remoteOther": false, "unknownCondition": false} {
		if _, ok := settings["core."+strings.ToLower(key)]; ok != want {
			t.Errorf("%s: included %v, want %v", key, ok, want)
		}
	}
}

func (r *testRepo) gitBool(args ...string) bool {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	err := command.Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false
	}
	r.t.Fatalf("git %v: %v", args, err)
	return false
}

func TestLoadConfigDotPatternUsesRealPath(t *testing.T) {
	fixture := newTestRepo(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(fixture.home, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeConfig(t, filepath.Join(fixture.home, "matched"), "[core]\n\tmatched = 1\n")
	repoLink := filepath.Join(fixture.home, "repo-link")
	if err := os.Symlink(fixture.root, repoLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	global := filepath.Join(alias, "gitconfig")
	writeConfig(t, global, "[includeIf \"gitdir:./repo-link/\"]\n\tpath = matched\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	settings := fixture.settings()
	ours := lastText(settings, "core.matched") == "1"
	theirs := fixture.gitBool("config", "--get", "core.matched")
	if ours != theirs {
		t.Fatalf("included by us %v, by git %v", ours, theirs)
	}
}

func TestLoadConfigRemoteURLFromLaterFile(t *testing.T) {
	fixture := newTestRepo(t)
	writeConfig(t, filepath.Join(fixture.home, "matched"), "[core]\n\tmatched = 1\n")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[includeIf \"hasconfig:remote.*.url:https://example.com/**\"]\n\tpath = matched\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	fixture.git("config", "remote.origin.url", "https://example.com/repo")
	settings := fixture.settings()
	if lastText(settings, "core.matched") != "1" || !fixture.gitBool("config", "--get", "core.matched") {
		t.Fatalf("hasconfig include ignored the repository config: %v", settings["core.matched"])
	}
}

func TestLoadConfigForbidsRemoteURLInRemoteInclude(t *testing.T) {
	fixture := newTestRepo(t)
	writeConfig(t, filepath.Join(fixture.home, "remotes"), "[remote \"upstream\"]\n\turl = https://example.com/other\n")
	fixture.git("config", "remote.origin.url", "https://example.com/repo")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[includeIf \"hasconfig:remote.*.url:https://example.com/**\"]\n\tpath = remotes\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	probe := exec.CommandContext(t.Context(), "git", "config", "--get", "core.bare")
	probe.Dir = fixture.root
	var exit *exec.ExitError
	if err := probe.Run(); !errors.As(err, &exit) || exit.ExitCode() != 128 {
		t.Fatalf("git accepted a remote URL inside a hasconfig include: %v", err)
	}
	if repo, err := Open(fixture.root); err == nil {
		t.Fatalf("Open = %+v, want an error", repo)
	}
}

func TestLoadConfigUserHome(t *testing.T) {
	current, err := user.Current()
	if err != nil || current.Username == "" || current.HomeDir == "" {
		t.Skipf("current user unavailable: %v", err)
	}
	if _, err := user.Lookup(current.Username); err != nil {
		t.Skipf("user lookup unavailable: %v", err)
	}
	got, ok := expandHome("~"+current.Username+"/x", false)
	if !ok || got != current.HomeDir+"/x" {
		t.Fatalf("expandHome = %q, %v", got, ok)
	}
	if got, ok := expandHome("~nobody-"+current.Username+"/x", false); ok {
		t.Fatalf("unknown user expanded to %q", got)
	}
}

func TestLoadConfigIncludeDepth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loop")
	writeConfig(t, path, "[include]\n\tpath = loop\n")
	ctx, err := newIncludeContext("", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfigFiles([]string{path}, nil, ctx); err == nil {
		t.Fatal("a self-including config loaded")
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

func TestLoadConfigBareRepositoryConditions(t *testing.T) {
	fixture := newTestRepo(t, "--bare")
	writeConfig(t, filepath.Join(fixture.home, "worktree"), "[core]\n\tworktree = 1\n")
	writeConfig(t, filepath.Join(fixture.home, "gitdir"), "[core]\n\tgitdir = 1\n")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[includeIf \"worktree:**\"]\n\tpath = worktree\n[includeIf \"gitdir:**\"]\n\tpath = gitdir\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	repo := fixture.open()
	if repo.WorkTree != "" {
		t.Fatalf("WorkTree = %q, want empty", repo.WorkTree)
	}
	settings, err := repo.config()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"core.worktree", "core.gitdir"} {
		_, ours := settings[key]
		if theirs := fixture.gitBool("config", "--get", key); ours != theirs {
			t.Errorf("%s: included by us %v, by git %v", key, ours, theirs)
		}
	}
}

func TestLoadConfigSymlinkedHead(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("checkout", "--quiet", "-b", "feature/x")
	fixture.write("file", "")
	fixture.git("add", "file")
	fixture.git("commit", "--quiet", "-m", "base")
	head := filepath.Join(fixture.root, ".git", "HEAD")
	if err := os.Remove(head); err != nil {
		t.Fatal(err)
	}
	symlink(t, "refs/heads/feature/x", head)
	writeConfig(t, filepath.Join(fixture.home, "branch"), "[core]\n\tbranch = 1\n")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[includeIf \"onbranch:feature/**\"]\n\tpath = branch\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	settings, err := fixture.open().config()
	if err != nil {
		t.Fatal(err)
	}
	_, ours := settings["core.branch"]
	if theirs := fixture.gitBool("config", "--get", "core.branch"); ours != theirs {
		t.Fatalf("onbranch with a symlinked HEAD: included by us %v, by git %v", ours, theirs)
	}
	if !ours {
		t.Fatal("onbranch did not match the symlinked HEAD")
	}
}

func TestLoadConfigIncludesUnresolvedGitDir(t *testing.T) {
	fixture := newTestRepo(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(fixture.root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeConfig(t, filepath.Join(fixture.home, "alias"), "[core]\n\talias = 1\n")
	writeConfig(t, filepath.Join(fixture.home, "physical"), "[core]\n\tphysical = 1\n")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[includeIf \"gitdir:"+filepath.ToSlash(alias)+"/\"]\n\tpath = alias\n[includeIf \"gitdir:"+filepath.ToSlash(resolvedPath(t, fixture.root))+"/\"]\n\tpath = physical\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	repo, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := repo.config()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"core.alias", "core.physical"} {
		_, ours := settings[key]
		_, _, gitErr := gitIn(t, alias, "config", "--get", key)
		if theirs := gitErr == nil; ours != theirs {
			t.Errorf("%s from %s: included by us %v, by git %v", key, alias, ours, theirs)
		}
		if !ours && (key == "core.physical" || runtime.GOOS != "windows") {
			t.Errorf("%s: not included", key)
		}
	}
}

func TestLoadConfigHeadForms(t *testing.T) {
	for name, head := range map[string]string{
		"detached":   "",
		"spaced ref": "ref:\t  refs/heads/feature/x  \n",
		"other ref":  "ref: refs/remotes/origin/main\n",
		"junk":       "nonsense\n",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.git("checkout", "--quiet", "-b", "feature/x")
			fixture.commit()
			if head == "" {
				fixture.git("checkout", "--quiet", "--detach")
			} else {
				fixture.write(".git/HEAD", head)
			}
			writeConfig(t, filepath.Join(fixture.home, "any"), "[core]\n\tany = 1\n")
			writeConfig(t, filepath.Join(fixture.home, "feature"), "[core]\n\tfeature = 1\n")
			global := filepath.Join(fixture.home, "gitconfig")
			writeConfig(t, global, "[includeIf \"onbranch:**\"]\n\tpath = any\n[includeIf \"onbranch:feature/**\"]\n\tpath = feature\n")
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			repo, err := Open(fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if repo == nil {
				for _, key := range []string{"core.any", "core.feature"} {
					if fixture.gitBool("config", "--get", key) {
						t.Errorf("Open found no repository but git includes %s", key)
					}
				}
				return
			}
			settings, err := repo.config()
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"core.any", "core.feature"} {
				_, ours := settings[key]
				if theirs := fixture.gitBool("config", "--get", key); ours != theirs {
					t.Errorf("%s: included by us %v, by git %v", key, ours, theirs)
				}
			}
		})
	}
}

func TestLoadConfigReftableHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	if out, err := exec.CommandContext(t.Context(), "git", "-C", t.TempDir(), "init", "--quiet", "--ref-format=reftable").CombinedOutput(); err != nil {
		t.Skipf("reftable unsupported: %v: %s", err, out)
	}
	for name, tc := range map[string]struct {
		detach bool
		format string
	}{"branch": {}, "detached": {detach: true}, "sha256": {format: "--object-format=sha256"}, "sha256 detached": {detach: true, format: "--object-format=sha256"}} {
		t.Run(name, func(t *testing.T) {
			detach := tc.detach
			args := []string{"--ref-format=reftable"}
			if tc.format != "" {
				args = append(args, tc.format)
			}
			fixture := newTestRepo(t, args...)
			fixture.git("checkout", "--quiet", "-b", "main")
			fixture.commit()
			fixture.git("checkout", "--quiet", "-b", "feature/x")
			for i := range 3 {
				fixture.write("file", strconv.Itoa(i))
				fixture.git("commit", "--quiet", "--all", "-m", "Fixture")
				fixture.git("checkout", "--quiet", "main")
				fixture.git("checkout", "--quiet", "feature/x")
			}
			if detach {
				fixture.git("checkout", "--quiet", "--detach")
			}
			head, err := os.ReadFile(filepath.Join(fixture.root, ".git", "HEAD"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(head), ".invalid") {
				t.Fatalf("HEAD stub = %q", head)
			}
			conditions := map[string]string{"any": "onbranch:**", "feature": "onbranch:feature/**", "main": "onbranch:main", "invalid": "onbranch:.invalid"}
			var global strings.Builder
			for key, condition := range conditions {
				writeConfig(t, filepath.Join(fixture.home, key), "[core]\n\t"+key+" = 1\n")
				fmt.Fprintf(&global, "[includeIf %q]\n\tpath = %s\n", condition, key)
			}
			path := filepath.Join(fixture.home, "gitconfig")
			writeConfig(t, path, global.String())
			t.Setenv("GIT_CONFIG_GLOBAL", path)
			repo := fixture.open()
			settings, err := repo.config()
			if err != nil {
				t.Fatal(err)
			}
			for key := range conditions {
				_, ours := settings["core."+key]
				if theirs := fixture.gitBool("config", "--get", "core."+key); ours != theirs {
					t.Errorf("%s: included by us %v, by git %v", conditions[key], ours, theirs)
				}
			}
			if _, ok := settings["core.feature"]; ok == detach {
				t.Errorf("onbranch:feature/** included %v with detach %v", ok, detach)
			}
			if _, ok := settings["core.invalid"]; ok {
				t.Error("onbranch:.invalid matched the HEAD stub")
			}
			reads := repo.ReadFiles()
			if !slices.Contains(reads, filepath.Join(repo.GitDir, "reftable", "tables.list")) {
				t.Errorf("tables.list missing from ReadFiles %q", reads)
			}
		})
	}
}

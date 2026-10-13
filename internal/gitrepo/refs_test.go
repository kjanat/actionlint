package gitrepo

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestValidRefname(t *testing.T) {
	for name, want := range map[string]bool{
		"refs/heads/main":      true,
		"HEAD":                 true,
		"refs/heads/a.b":       true,
		"refs/heads/@":         true,
		"refs/heads/a@b":       true,
		"":                     false,
		"@":                    false,
		"refs/heads/":          false,
		"/refs/heads/x":        false,
		"refs//heads":          false,
		"refs/heads/.hidden":   false,
		"refs/heads/a..b":      false,
		"refs/heads/a.lock":    false,
		"refs/heads/a@{b}":     false,
		"refs/heads/a b":       false,
		"refs/heads/a\tb":      false,
		"refs/heads/a~b":       false,
		"refs/heads/a^b":       false,
		"refs/heads/a:b":       false,
		"refs/heads/a?b":       false,
		"refs/heads/a*b":       false,
		"refs/heads/a[b":       false,
		"refs/heads/a\\b":      false,
		"refs/heads/a\x7fb":    false,
		"refs/heads/a\x01b":    false,
		"refs/heads/ends.":     false,
		"refs/heads/x.lock/y":  false,
		"refs/heads/é":         true,
		"refs/heads/feature/x": true,
	} {
		if got := validRefname(name, true); got != want {
			t.Errorf("validRefname(%q, one level) = %v, want %v", name, got, want)
		}
	}
	if validRefname("HEAD", false) || !validRefname("refs/x", false) {
		t.Error("validRefname without one level mishandles component counts")
	}
}

func TestAbsolutePathOn(t *testing.T) {
	for _, tc := range []struct {
		goos, path string
		want       bool
	}{
		{"linux", "/x", true},
		{"linux", `\x`, false},
		{"linux", "C:x", false},
		{"linux", "C:/x", false},
		{"linux", "x", false},
		{"darwin", `\x`, false},
		{"windows", "/x", true},
		{"windows", `\x`, true},
		{"windows", "C:x", true},
		{"windows", "C:/x", true},
		{"windows", "c:", true},
		{"windows", "x", false},
		{"windows", "1:x", false},
		{"windows", "", false},
	} {
		if got := absolutePathOn(tc.goos, tc.path); got != tc.want {
			t.Errorf("absolutePathOn(%s, %q) = %v, want %v", tc.goos, tc.path, got, tc.want)
		}
	}
}

func TestRefPath(t *testing.T) {
	repo := &Repository{GitDir: filepath.Join("common", "worktrees", "wt"), CommonDir: "common"}
	for name, want := range map[string]string{
		"HEAD":                       filepath.Join("common", "worktrees", "wt", "HEAD"),
		"CHERRY_PICK_HEAD":           filepath.Join("common", "worktrees", "wt", "CHERRY_PICK_HEAD"),
		"refs/bisect/bad":            filepath.Join("common", "worktrees", "wt", "refs", "bisect", "bad"),
		"refs/worktree/x":            filepath.Join("common", "worktrees", "wt", "refs", "worktree", "x"),
		"refs/rewritten/x":           filepath.Join("common", "worktrees", "wt", "refs", "rewritten", "x"),
		"refs/heads/main":            filepath.Join("common", "refs", "heads", "main"),
		"refs/meta/x":                filepath.Join("common", "refs", "meta", "x"),
		"worktrees/other/HEAD":       filepath.Join("common", "worktrees", "other", "HEAD"),
		"worktrees/other":            filepath.Join("common", "worktrees", "other"),
		"worktrees/other/refs/heads": filepath.Join("common", "worktrees", "other", "refs", "heads"),
		"main-worktree/HEAD":         filepath.Join("common", "HEAD"),
		"main-worktree/refs/heads/x": filepath.Join("common", "main-worktree", "refs", "heads", "x"),
	} {
		if got := repo.refPath(name); got != want {
			t.Errorf("refPath(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLoadConfigSymrefChains(t *testing.T) {
	type step struct{ args []string }
	symref := func(name, target string) step { return step{[]string{"symbolic-ref", name, target}} }
	update := func(name, target string) step { return step{[]string{"update-ref", name, target}} }
	cases := map[string]struct {
		steps []step
		want  map[string]bool
	}{
		"direct":          {want: map[string]bool{"main": true, "any": true}},
		"through meta":    {steps: []step{symref("refs/meta/current", "refs/heads/main"), symref("HEAD", "refs/meta/current")}, want: map[string]bool{"main": true, "any": true}},
		"through missing": {steps: []step{symref("refs/meta/current", "refs/heads/missing"), symref("HEAD", "refs/meta/current")}, want: map[string]bool{"main": false, "any": true}},
		"meta is oid":     {steps: []step{update("refs/meta/current", "HEAD"), symref("HEAD", "refs/meta/current")}, want: map[string]bool{"main": false, "any": false}},
		"dangling":        {steps: []step{symref("HEAD", "refs/meta/gone")}, want: map[string]bool{"main": false, "any": false}},
		"three hops":      {steps: []step{symref("refs/s3", "refs/heads/main"), symref("refs/s2", "refs/s3"), symref("refs/s1", "refs/s2"), symref("HEAD", "refs/s1")}, want: map[string]bool{"main": true, "any": true}},
		"four hops":       {steps: []step{symref("refs/s4", "refs/heads/main"), symref("refs/s3", "refs/s4"), symref("refs/s2", "refs/s3"), symref("refs/s1", "refs/s2"), symref("HEAD", "refs/s1")}, want: map[string]bool{"main": false, "any": false}},
		"loop":            {steps: []step{symref("refs/s2", "refs/s1"), symref("refs/s1", "refs/s2"), symref("HEAD", "refs/s1")}, want: map[string]bool{"main": false, "any": false}},
	}
	for _, format := range []string{"files", "reftable"} {
		for name, tc := range cases {
			t.Run(format+" "+name, func(t *testing.T) {
				fixture := newTestRepo(t, "--ref-format="+format, "--initial-branch=main")
				fixture.commit()
				for _, step := range tc.steps {
					fixture.git(step.args...)
				}
				fixture.compareBranchIncludes(tc.want)
			})
		}
	}
}

func TestLoadConfigLooseRefForms(t *testing.T) {
	cases := map[string]struct {
		content string
		link    string
		dir     bool
		want    map[string]bool
	}{
		"spaced":         {content: "ref:\t \trefs/heads/main  \n", want: map[string]bool{"main": true, "any": true}},
		"oid with tail":  {content: strings.Repeat("0", 40) + "\tbranch 'main'\n", want: map[string]bool{"main": false, "any": false}},
		"junk":           {content: "nonsense\n", want: map[string]bool{"main": false, "any": false}},
		"short oid":      {content: strings.Repeat("0", 39) + "\n", want: map[string]bool{"main": false, "any": false}},
		"oid then text":  {content: strings.Repeat("0", 40) + "x\n", want: map[string]bool{"main": false, "any": false}},
		"bad target":     {content: "ref: refs/heads/bad..name\n", want: map[string]bool{"main": false, "any": false}},
		"directory":      {dir: true, want: map[string]bool{"main": false, "any": false}},
		"symlink":        {link: "refs/heads/main", want: map[string]bool{"main": true, "any": true}},
		"bad symlink":    {link: "refs/heads/bad..name", want: map[string]bool{"main": false, "any": false}},
		"other symlink":  {link: "../elsewhere", want: map[string]bool{"main": false, "any": false}},
		"empty symref":   {content: "ref:\n", want: map[string]bool{"main": false, "any": false}},
		"onelevel chain": {content: "ref: ONE_LEVEL\n", want: map[string]bool{"main": false, "any": false}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if (tc.link != "" || tc.dir) && runtime.GOOS == "windows" {
				t.Skip("symlinked and directory refs need a POSIX file system")
			}
			fixture := newTestRepo(t, "--initial-branch=main")
			fixture.commit()
			fixture.git("symbolic-ref", "HEAD", "refs/meta/current")
			path := filepath.Join(fixture.root, ".git", "refs", "meta", "current")
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.dir:
				if err := os.Mkdir(path, 0o750); err != nil {
					t.Fatal(err)
				}
			case tc.link != "":
				if err := os.Symlink(tc.link, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			default:
				fixture.write(".git/refs/meta/current", tc.content)
			}
			fixture.compareBranchIncludes(tc.want)
		})
	}
}

func (r *testRepo) compareBranchIncludes(want map[string]bool) {
	r.t.Helper()
	writeConfig(r.t, filepath.Join(r.home, "main"), "[core]\n\tmain = 1\n")
	writeConfig(r.t, filepath.Join(r.home, "any"), "[core]\n\tany = 1\n")
	global := filepath.Join(r.home, "gitconfig")
	writeConfig(r.t, global, "[includeIf \"onbranch:main\"]\n\tpath = main\n[includeIf \"onbranch:**\"]\n\tpath = any\n")
	r.t.Setenv("GIT_CONFIG_GLOBAL", global)
	repo, err := Open(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	if repo == nil {
		r.t.Fatal("Open found no repository")
	}
	settings, err := repo.config()
	if err != nil {
		r.t.Fatal(err)
	}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		_, ours := settings["core."+key]
		if theirs := r.gitBool("config", "--get", "core."+key); ours != theirs {
			r.t.Errorf("onbranch include %s: ours %v, git %v", key, ours, theirs)
		}
		if ours != want[key] {
			r.t.Errorf("onbranch include %s = %v, want %v", key, ours, want[key])
		}
	}
}

func TestLoadConfigReftableMultiBlock(t *testing.T) {
	fixture := newTestRepo(t, "--ref-format=reftable", "--initial-branch=main")
	fixture.commit()
	oid, _, err := gitIn(t, fixture.root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var updates strings.Builder
	for i := range 400 {
		fmt.Fprintf(&updates, "create A_%03d %s\n", i, oid)
	}
	fixture.gitInput(updates.String(), "update-ref", "--stdin")
	fixture.git("pack-refs")
	list, err := os.ReadFile(filepath.Join(fixture.root, ".git", "reftable", "tables.list"))
	if err != nil {
		t.Fatal(err)
	}
	tables := strings.Fields(string(list))
	if len(tables) != 1 {
		t.Fatalf("tables after pack-refs = %q", tables)
	}
	data, err := os.ReadFile(filepath.Join(fixture.root, ".git", "reftable", tables[0]))
	if err != nil {
		t.Fatal(err)
	}
	blockSize := uint24(data[5:])
	if blockSize == 0 || len(data) < 2*blockSize || data[blockSize] != 'r' {
		t.Fatalf("the table does not have a second ref block: size %d, block size %d", len(data), blockSize)
	}
	if _, present, err := reftableBlockRefOnly(data, blockSize, "HEAD"); err != nil || present {
		t.Fatalf("HEAD sits in the first ref block: %v, %v", present, err)
	}
	fixture.compareBranchIncludes(map[string]bool{"main": true, "any": true})
}

func reftableBlockRefOnly(data []byte, blockSize int, name string) (refValue, bool, error) {
	end := uint24(data[reftableHeaderV1+1:])
	value, present, _, err := reftableBlockRef(data[reftableHeaderV1+4:end], 20, name)
	return value, present, err
}

package workflownames

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWorkflowPathsLinearIdentityChecks(t *testing.T) {
	dir := t.TempDir()
	candidates := make([]string, 1000)
	for i := range candidates {
		candidates[i] = filepath.Join(dir, fmt.Sprintf("workflow-%04d.yml", i))
	}
	calls, stats := 0, 0
	stat := func(path string) (os.FileInfo, error) { stats++; return os.Stat(path) }
	same := func(left, right string) bool {
		calls++
		return samePathOnFilesystem(left, right, false, stat, os.ReadDir)
	}
	if got := workflowPathsByIdentity(dir, candidates, same); !slices.Equal(got, candidates) {
		t.Fatal("workflow inventory changed")
	}
	if calls > 2*len(candidates) || stats > 4*len(candidates) {
		t.Fatalf("quadratic identity work: %d comparisons, %d stats for %d paths", calls, stats, len(candidates))
	}
}

func TestWorkflowFilenameFoldKeys(t *testing.T) {
	for _, names := range [][2]string{{"build.yml", "BUILD.yml"}, {"s.yml", "ſ.yml"}, {"K.yml", "K.yml"}, {"Σ.yml", "ς.yml"}, {"ß.yml", "ẞ.yml"}} {
		if !strings.EqualFold(names[0], names[1]) || filenameKey(names[0]) != filenameKey(names[1]) {
			t.Fatalf("case-fold bucket differs for %q", names)
		}
		if got := workflowPaths("repo", []string{filepath.Join("repo", names[0]), filepath.Join("repo", names[1])}, true); len(got) != 1 || got[0] != filepath.Join("repo", names[1]) {
			t.Fatalf("case-fold alias did not select last overlay: %v", got)
		}
	}
}

func BenchmarkWorkflowPathsDistinct(b *testing.B) {
	dir := b.TempDir()
	candidates := make([]string, 1000)
	for i := range candidates {
		candidates[i] = filepath.Join(dir, fmt.Sprintf("workflow-%04d.yml", i))
	}
	b.ResetTimer()
	for b.Loop() {
		workflowPaths(dir, candidates, false)
	}
}

func TestIndexSymlinkCheckout(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "checkout")
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "linked-checkout")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	disk := filepath.Join(dir, "build.yml")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(alias, ".github", "workflows", "build.yml")
	missing := filepath.Join(alias, ".github", "workflows", "new.yml")
	for _, path := range []string{disk, filepath.Join(dir, "new.yml")} {
		other := filepath.Join(alias, ".github", "workflows", filepath.Base(path))
		if !SamePath(path, other) || !SamePath(other, path) {
			t.Fatalf("checkout alias not recognized: %q, %q", path, other)
		}
	}
	loads := 0
	index := &Index{Paths: []string{overlay, missing}, Load: func(path string) (string, bool, error) {
		loads++
		if path != overlay && path != missing {
			t.Errorf("did not retain overlay spelling: %q", path)
		}
		return "", true, nil
	}}
	names, err := index.ForRoot(root)
	if err != nil || !names.Complete || len(names.Values) != 2 || !names.Values[".github/workflows/build.yml"] || !names.Values[".github/workflows/new.yml"] || loads != 2 {
		t.Fatalf("names=%+v err=%v loads=%d", names, err, loads)
	}
	if _, err := index.ForRoot(alias); err != nil || loads != 2 {
		t.Fatalf("aliased root missed inventory cache: loads=%d err=%v", loads, err)
	}
	if got := workflowPaths(dir, []string{disk, overlay, disk}, false); !slices.Equal(got, []string{disk}) {
		t.Fatalf("last overlay did not win: %v", got)
	}
}

func TestIndexUnnamedOverlaySpelling(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "build.yml")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "BUILD.yml")
	if runtime.GOOS != "windows" {
		overlay = disk
	}
	loads := 0
	index := &Index{Paths: []string{overlay}, Load: func(path string) (string, bool, error) {
		loads++
		if path != overlay {
			t.Errorf("loaded %q instead of original overlay %q", path, overlay)
		}
		return "", true, nil
	}}
	names, err := index.ForRoot(root)
	if err != nil || !names.Complete || len(names.Values) != 1 || !names.Values[".github/workflows/build.yml"] || loads != 1 {
		t.Fatalf("names=%+v err=%v loads=%d", names, err, loads)
	}
	if runtime.GOOS == "windows" {
		if _, err := index.ForRoot(strings.ToUpper(root)); err != nil || loads != 1 {
			t.Fatalf("case-alias root missed inventory cache: loads=%d err=%v", loads, err)
		}
	}
}

func TestWorkflowPathsPlatformIdentity(t *testing.T) {
	dir := filepath.Join("repo", ".github", "workflows")
	disk := filepath.Join(dir, "build.yml")
	overlay := filepath.Join("REPO", ".GITHUB", "WORKFLOWS", "BUILD.yml")
	other := filepath.Join(dir, "other.yaml")
	candidates := []string{disk, other, overlay,
		filepath.Join(dir, "nested", "ignored.yml"), filepath.Join(dir, "README.md")}
	for _, windows := range []bool{false, true} {
		same := missingPathIdentity(windows)
		want := []string{disk, other}
		if windows {
			want = []string{overlay, other}
		}
		slices.Sort(want)
		if got := workflowPathsByIdentity(dir, candidates, same); !slices.Equal(got, want) {
			t.Fatalf("windows=%v: paths=%v, want %v", windows, got, want)
		}
		if same(overlay, disk) != windows || same(disk, overlay) != windows {
			t.Fatalf("missing-path fallback differs for windows=%v", windows)
		}
	}
}

func TestWorkflowPathsLastOverlayWins(t *testing.T) {
	dir := filepath.Join("repo", ".github", "workflows")
	disk := filepath.Join(dir, "build.yml")
	first := filepath.Join(dir, "BUILD.yml")
	last := filepath.Join(dir, "Build.yml")
	if got := workflowPathsByIdentity(dir, []string{disk, first, last}, missingPathIdentity(true)); !slices.Equal(got, []string{last}) {
		t.Fatalf("last in-memory spelling must win: %v", got)
	}
	want := []string{disk, first, last}
	slices.Sort(want)
	if got := workflowPathsByIdentity(dir, []string{disk, first, last}, missingPathIdentity(false)); !slices.Equal(got, want) {
		t.Fatalf("case-sensitive paths must remain distinct: %v", got)
	}
}

// Model the platform fallback without evidence from the host filesystem.
func missingPathIdentity(windows bool) func(string, string) bool {
	stat := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	readDir := func(string) ([]os.DirEntry, error) { return nil, os.ErrNotExist }
	return func(left, right string) bool {
		return samePathWithCaseSensitivity(left, right, windows, stat, readDir, func(string) bool { return false })
	}
}

func TestWorkflowPathsHostFilesystemIdentity(t *testing.T) {
	dir := t.TempDir()
	disk := filepath.Join(dir, "build.yml")
	overlay := filepath.Join(dir, "BUILD.yml")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	diskInfo, err := os.Stat(disk)
	if err != nil {
		t.Fatal(err)
	}
	overlayInfo, overlayErr := os.Stat(overlay)
	filesystemAlias := overlayErr == nil && os.SameFile(diskInfo, overlayInfo)
	for _, windows := range []bool{false, true} {
		want := []string{disk, overlay}
		if filesystemAlias || windows {
			want = []string{overlay}
		}
		slices.Sort(want)
		if got := workflowPaths(dir, []string{disk, overlay}, windows); !slices.Equal(got, want) {
			t.Fatalf("filesystemAlias=%v windows=%v: got %v, want %v", filesystemAlias, windows, got, want)
		}
	}
	if got := SamePath(disk, overlay); got != (filesystemAlias || runtime.GOOS == "windows") {
		t.Fatalf("host equality=%v, filesystemAlias=%v on %s", got, filesystemAlias, runtime.GOOS)
	}
}

func TestWorkflowPathsUppercaseExtensionIgnored(t *testing.T) {
	dir := filepath.Join("repo", ".github", "workflows")
	valid := filepath.Join(dir, "build.yml")
	for _, windows := range []bool{false, true} {
		candidates := []string{valid, filepath.Join(dir, "producer.YML"), filepath.Join(dir, "other.YAML"), filepath.Join(dir, "mixed.yMl")}
		if got := workflowPaths(dir, candidates, windows); !slices.Equal(got, []string{valid}) {
			t.Fatalf("windows=%v: uppercase extension was indexed: %v", windows, got)
		}
	}
}

func TestIndexFilesystemCaseAlias(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "build.yml")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "BUILD.yml")
	if _, err := os.Stat(overlay); err != nil {
		t.Skip("requires a case-insensitive filesystem")
	}
	for _, name := range []string{"New name", ""} {
		loads := 0
		index := &Index{Paths: []string{overlay}, Load: func(path string) (string, bool, error) {
			loads++
			if path != overlay {
				t.Errorf("loaded stale disk spelling %q", path)
				return "", false, nil
			}
			return name, true, nil
		}}
		got, err := index.ForRoot(root)
		if err != nil || !got.Complete || loads != 1 || len(got.Values) != 1 {
			t.Fatalf("case alias inventory=%+v err=%v loads=%d", got, err, loads)
		}
		if name != "" && !got.Values[name] {
			t.Fatalf("missing overlay name: %+v", got)
		}
		if name == "" && !got.Values[".github/workflows/build.yml"] {
			t.Fatalf("disk basename changed through overlay: %+v", got)
		}
		if _, err := index.ForRoot(strings.ToUpper(root)); err != nil || loads != 1 {
			t.Fatalf("root casing alias reloaded inventory: loads=%d err=%v", loads, err)
		}
	}
}

func TestWorkflowPathsDistinctFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "build.yml")
	other := filepath.Join(dir, "other.yml")
	if err := os.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, other); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	want := []string{first, other}
	if got := workflowPaths(dir, want, false); !slices.Equal(got, want) {
		t.Fatalf("distinct hardlink producer names collapsed: %v", got)
	}
	upper := filepath.Join(dir, "BUILD.yml")
	if _, err := os.Stat(upper); err == nil {
		return
	}
	if err := os.Link(first, upper); err != nil {
		t.Fatal(err)
	}
	want = append(want, upper)
	slices.Sort(want)
	if got := workflowPaths(dir, want, false); !slices.Equal(got, want) {
		t.Fatalf("case-sensitive hardlink producers collapsed: %v", got)
	}
	if err := os.Remove(upper); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upper, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := workflowPaths(dir, want, false); !slices.Equal(got, want) {
		t.Fatalf("case-sensitive separate producers collapsed: %v", got)
	}
}

func TestSamePathFilesystemIdentity(t *testing.T) {
	dir := t.TempDir()
	lower, upper := filepath.Join(dir, "build.yml"), filepath.Join(dir, "BUILD.yml")
	if err := os.WriteFile(lower, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(lower)
	if err != nil {
		t.Fatal(err)
	}
	stat := func(string) (os.FileInfo, error) { return info, nil }
	if !samePathOnFilesystem(lower, upper, false, stat, os.ReadDir) {
		t.Fatal("filesystem case alias treated as a separate path")
	}
	identity := func(left, right string) bool { return samePathOnFilesystem(left, right, false, stat, os.ReadDir) }
	if got := workflowPathsByIdentity(dir, []string{lower, upper}, identity); !slices.Equal(got, []string{upper}) {
		t.Fatalf("last overlay did not replace disk alias: %v", got)
	}
	if samePathOnFilesystem(lower, filepath.Join(dir, "other.yml"), false, stat, os.ReadDir) {
		t.Fatal("unrelated hardlink spelling collapsed")
	}
	missing := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	if samePathOnFilesystem(lower, upper, false, missing, os.ReadDir) {
		t.Fatal("missing overlay spelling treated as a case alias")
	}
}

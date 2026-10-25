package shellcheck

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestPluginSourceGrantsAndMapping(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	file := writeShellcheckFixture(t, root, "lib/clean.sh", "VALUE=one\n")
	if err := os.Mkdir(filepath.Join(root, "lib", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("lib", "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	external := writeShellcheckFixture(t, outside, "shared.sh", "SHARED=two\n")
	cfg, err := config.Parse([]byte(fmt.Sprintf("tools: {shellcheck: {config: {external-sources: true, source-path: [%s, %s]}}}", strconv.Quote(outside), strconv.Quote(filepath.Join(root, "missing")))))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	reader := fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
	script := tools.Script{Path: "ci.yml", Config: cfg, Scope: &tools.Scope{Workspace: root, AnalysisDir: root, Reader: reader}, Execution: tools.Execution{Directory: root, DirectoryKnown: true}, Key: &workflowlang.Pos{Line: 6}, Value: &workflowlang.Pos{Line: 6}}
	sources, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, requestedSourceGrants(script.Config))
	if err != nil {
		t.Fatal(err)
	}
	defer sources.close()
	input := analyzer.Input{Options: []byte("{}"), Scripts: []analyzer.Script{{Runtime: &analyzer.Runtime{Directory: root, DirectoryKnown: true}}}}
	if err := sources.configure(&input); err != nil {
		t.Fatal(err)
	}
	if input.Scripts[0].Runtime.Directory != "/workspace" || len(input.Files) != 2 {
		t.Fatalf("wrong grants: %+v", input)
	}
	for _, read := range []struct {
		mount int
		name  string
	}{{0, "lib/clean.sh"}, {1, "shared.sh"}} {
		if _, err := fs.ReadFile(input.Files[read.mount].FS, read.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{sourcePath(file, root), sourcePath(external, outside), filepath.Join(root, "missing")} {
		if !slices.Contains(inputs, path) {
			t.Fatalf("dependency missing: %s from %v", path, inputs)
		}
	}
	finding, err := sources.finding(diagnostic.Diagnostic{Path: "/workspace/lib/./clean.sh", Start: diagnostic.Position{Line: 1, Column: 1}}, script, 3)
	if err != nil || finding.Path != sourcePath(file, root) || !finding.RawScript || string(finding.Content) != "VALUE=one\n" || finding.Script != 3 {
		t.Fatalf("mapping=%+v error=%v", finding, err)
	}
	dir, err := input.Files[0].FS.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	open, ok := dir.(interface {
		OpenAt(string, int, fs.FileMode) (fs.File, error)
	})
	if !ok {
		t.Fatal("directory lacks relative open")
	}
	linked, err := open.OpenAt("link/../clean.sh", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := linked.Close(); err != nil {
		t.Fatal(err)
	}
	finding, err = sources.finding(diagnostic.Diagnostic{Path: "/workspace/link/../clean.sh"}, script, 3)
	if err != nil || finding.Path != sourcePath(file, root) || string(finding.Content) != "VALUE=one\n" {
		t.Fatalf("symlink mapping=%+v error=%v", finding, err)
	}
	if err := os.WriteFile(file, []byte("VALUE=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, requestedSourceGrants(script.Config))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.close()
	content, err := fs.ReadFile(fresh.mounts[0].files, "lib/clean.sh")
	if err != nil || string(content) != "VALUE=changed\n" {
		t.Fatalf("stale source: %q %v", content, err)
	}
}

func TestPluginSourceGrantResolvesDirectorySymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	file := writeShellcheckFixture(t, outside, "shared.sh", "SHARED=two\n")
	alias := filepath.Join(root, "shared")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outside, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(outside, "sub"), link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, directory, grant string }{
		{"resolved working directory", resolved, alias},
		{"aliased working directory", alias, alias},
		{"symlink before parent", resolved, link + string(os.PathSeparator) + ".."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(fmt.Sprintf("tools: {shellcheck: {config: {external-sources: true, source-path: [%s, %s, %s]}}}", strconv.Quote(file), strconv.Quote(filepath.Join(root, "missing")), strconv.Quote(tc.grant))))
			if err != nil {
				t.Fatal(err)
			}
			var inputs []string
			reader := fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
			script := tools.Script{Path: "ci.yml", Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: reader}, Execution: tools.Execution{Directory: tc.directory, DirectoryKnown: true}, Key: &workflowlang.Pos{Line: 1}, Value: &workflowlang.Pos{Line: 1}}
			sources, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, requestedSourceGrants(script.Config))
			if err != nil {
				t.Fatal(err)
			}
			defer sources.close()
			input := analyzer.Input{Options: []byte("{}"), Scripts: []analyzer.Script{{Runtime: &analyzer.Runtime{Directory: tc.directory, DirectoryKnown: true}}}}
			if err := sources.configure(&input); err != nil {
				t.Fatal(err)
			}
			if input.Scripts[0].Runtime.Directory != "/sources/2" || len(input.Files) != 2 {
				t.Fatalf("working directory not granted: %s", input.Scripts[0].Runtime.Directory)
			}
			for _, host := range []string{tc.grant, resolved} {
				if !slices.Contains(input.SourceRoots, analyzer.SourceRoot{Host: filepath.ToSlash(host), Guest: "/sources/2"}) {
					t.Fatalf("source root missing: %s from %+v", host, input.SourceRoots)
				}
				for _, mapping := range input.SourceRoots {
					if strings.HasPrefix(filepath.ToSlash(host)+"/shared.sh", mapping.Host+"/") {
						if mapping.Guest != "/sources/2" {
							t.Fatalf("source alias shadowed: %s by %+v", host, mapping)
						}
						break
					}
				}
			}
			if _, err := fs.ReadFile(input.Files[1].FS, "shared.sh"); err != nil {
				t.Fatal(err)
			}
			dependencies := []string{sourcePath(file, outside)}
			if tc.name == "symlink before parent" {
				dependencies = append(dependencies, filepath.Join(sourcePath(root, root), "link"))
			} else {
				dependencies = append(dependencies, filepath.Join(sourcePath(root, root), "shared"))
			}
			for _, dependency := range dependencies {
				if !slices.Contains(inputs, dependency) {
					t.Fatalf("dependency missing: %s from %v", dependency, inputs)
				}
			}
			finding, err := sources.finding(diagnostic.Diagnostic{Path: "/sources/2/shared.sh"}, script, 0)
			if err != nil || finding.Path != filepath.Join(resolved, "shared.sh") || string(finding.Content) != "SHARED=two\n" {
				t.Fatalf("source mapping=%+v error=%v", finding, err)
			}
		})
	}
}

func TestPluginWorkingDirectoryCannotGrantOutsideWorkspace(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	cfg, err := config.Parse([]byte("tools: {shellcheck: {config: {external-sources: true}}}"))
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: fsread.New(t.Context(), nil, nil)}, Execution: tools.Execution{Directory: outside, DirectoryKnown: true}}
	if _, err := (documentPluginRunner{}).sourceFiles(context.Background(), script, requestedSourceGrants(script.Config)); err == nil || !strings.Contains(err.Error(), "outside its source grants") {
		t.Fatalf("outside cwd granted: %v", err)
	}
	cfg, err = config.Parse([]byte(fmt.Sprintf("tools: {shellcheck: {config: {external-sources: true, source-path: [%s]}}}", strconv.Quote(outside))))
	if err != nil {
		t.Fatal(err)
	}
	script.Config = cfg
	sources, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, requestedSourceGrants(script.Config))
	if err != nil {
		t.Fatal(err)
	}
	defer sources.close()
	if sources.directory != "/sources/0" {
		t.Fatalf("explicit directory not mapped: %s", sources.directory)
	}
}

func requestedSourceGrants(cfg *config.Config) []analyzer.ToolResourceRequest {
	resources := []analyzer.ToolResourceRequest{{Kind: analyzer.ToolWorkspace}}
	for _, path := range cfg.Tools.Shellcheck.Config.Inline().SourcePath {
		resources = append(resources, analyzer.ToolResourceRequest{Kind: analyzer.ToolDirectory, Path: path})
	}
	return resources
}

func TestPluginWorkingDirectoryUsesMostSpecificGrant(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte("tools: {shellcheck: {config: {external-sources: true}}}"))
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: fsread.New(t.Context(), nil, nil)}, Execution: tools.Execution{Directory: nested, DirectoryKnown: true}}
	files, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, []analyzer.ToolResourceRequest{{Kind: analyzer.ToolWorkspace}, {Kind: analyzer.ToolDirectory, Path: nested}})
	if err != nil {
		t.Fatal(err)
	}
	defer files.close()
	if files.directory != "/sources/0" {
		t.Fatalf("directory=%s", files.directory)
	}
}

func TestPluginRequestsCannotExpandHostSourceAuthority(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	nested := filepath.Join(workspace, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config, requested string
		allowed                 bool
	}{
		{"no host permission", "{}", workspace, false},
		{"explicit denial", "{external-sources: false}", workspace, false},
		{"arbitrary external directory", "{external-sources: true}", outside, false},
		{"symlink escape", "{external-sources: true}", escape, false},
		{"nested directory", "{external-sources: true}", nested, true},
		{"explicit external root", "{source-path: [" + strconv.Quote(outside) + "]}", outside, true},
		{"explicit symlink root", "{source-path: [" + strconv.Quote(escape) + "]}", escape, true},
		{"denial overrides configured paths", "{external-sources: false, source-path: [" + strconv.Quote(outside) + "]}", outside, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte("tools: {shellcheck: {config: " + tc.config + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			var recorded []string
			script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: workspace, Reader: fsread.New(t.Context(), nil, func(name string) { recorded = append(recorded, name) })}, Execution: tools.Execution{Directory: workspace, DirectoryKnown: true}}
			resources := []analyzer.ToolResourceRequest{{Kind: analyzer.ToolWorkspace}, {Kind: analyzer.ToolDirectory, Path: tc.requested}}
			files, err := (documentPluginRunner{}).sourceFiles(t.Context(), script, resources)
			if !tc.allowed {
				if err == nil || files != nil {
					t.Fatalf("unauthorized grant accepted: files=%v err=%v", files, err)
				}
				if slices.Contains(recorded, outside) {
					t.Fatalf("unauthorized dependency recorded: %v", recorded)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := files.close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

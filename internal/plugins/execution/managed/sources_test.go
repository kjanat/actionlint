package managed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	inputfiles "actionlint.kjanat.dev/v2/internal/input/files"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// sourceRunner is a document runner for the provider named shellcheck,
// whose tools.shellcheck.config grants source access.
var sourceRunner = documentRunner{documentRoute: shellcheckRoute}

func TestPluginFindingUsesCapturedScriptReferenceAcrossPathSpellings(t *testing.T) {
	t.Parallel()
	reference := sourcepos.Reference{Document: "captured-action", Revision: "captured-revision", Node: "run", Address: "runs.steps[0].run"}
	script := tools.Script{Path: filepath.Join(t.TempDir(), "action.yml"), Content: []byte("captured action"), Fact: checkinputs.Script{Reference: reference}}
	files := pluginSourceFiles{}
	finding := results.Diagnostic{Path: "actions/example/action.yml", Reference: &reference, Message: "shell issue"}
	got, err := files.finding("shellcheck", finding, script, 2)
	if err != nil || got.Path != script.Path || got.Reference == nil || *got.Reference != reference || got.Script != 2 || string(got.Content) != string(script.Content) || got.RawScript || got.UnavailableSource.Present() {
		t.Fatalf("captured script mapping=%+v error=%v", got, err)
	}
	for _, foreign := range []*sourcepos.Reference{nil, {}, {Document: "other", Revision: reference.Revision, Node: reference.Node}, {Document: reference.Document, Revision: "stale", Node: reference.Node}, {Document: reference.Document, Revision: reference.Revision, Node: "other"}} {
		finding.Reference = foreign
		if _, err := files.finding("shellcheck", finding, script, 2); err == nil {
			t.Fatalf("accepted a path without the selected script reference: %+v", foreign)
		}
	}
}

func TestPluginSourceGrantsAndMapping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "shared-sources")
	file := fixturefiles.Write(t, root, "lib/clean.sh", "VALUE=one\n")
	if err := os.Mkdir(filepath.Join(root, "lib", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("lib", "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	external := fixturefiles.Write(t, shared, "shared.sh", "SHARED=two\n")
	cfg, err := (config.RuleSet{}).Parse(fmt.Appendf(nil, "tools: {shellcheck: {config: {external-sources: true, source-path: [%s, %s]}}}", strconv.Quote(shared), strconv.Quote(filepath.Join(root, "missing"))))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	reader := inputfiles.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
	script := tools.Script{Path: "ci.yml", Config: cfg, Scope: &tools.Scope{Workspace: root, AnalysisDir: root, Reader: reader}, Execution: tools.Execution{Directory: root, DirectoryKnown: true}, Key: &workflowlang.Pos{Line: 6}, Value: &workflowlang.Pos{Line: 6}}
	sources, err := sourceRunner.sourceFiles(t.Context(), script, requestedSourceGrants(t, script.Config))
	if err != nil {
		t.Fatal(err)
	}
	defer sources.close()
	input := checkinputs.Input{Options: []byte("{}"), Scripts: []checkinputs.Script{{Runtime: &scriptmodel.Execution{Directory: root, DirectoryKnown: true}}}}
	sources.configure(&input)
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
	for _, path := range []string{sourcePath(file, root), sourcePath(external, shared), filepath.Join(root, "missing")} {
		if !slices.Contains(inputs, path) {
			t.Fatalf("dependency missing: %s from %v", path, inputs)
		}
	}
	finding, err := sources.finding("shellcheck", results.Diagnostic{Path: "/workspace/lib/./clean.sh", Start: sourcepos.Position{Line: 1, Column: 1}, Fixes: []results.Fix{{Description: "must not survive"}}}, script, 3)
	if err != nil || finding.Path != sourcePath(file, root) || !finding.RawScript || string(finding.Content) != "VALUE=one\n" || finding.Script != 3 {
		t.Fatalf("mapping=%+v error=%v", finding, err)
	}
	if !finding.UnavailableSource.Present() || finding.UnavailableSource.Provider() != "shellcheck" || finding.UnavailableSource.Reason() != "not-captured" || finding.Reference != nil || len(finding.Fixes) != 0 {
		t.Fatalf("plugin grant source fabricated revision evidence: %+v", finding)
	}
	for _, path := range []string{"/workspace/lib/unread.sh", "/ungranted/clean.sh"} {
		finding, err := sources.finding("shellcheck", results.Diagnostic{Path: path}, script, 3)
		if err == nil || finding.UnavailableSource.Present() {
			t.Fatalf("unread or ungranted source acquired an unavailable allowance: path=%s finding=%+v error=%v", path, finding, err)
		}
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
	finding, err = sources.finding("shellcheck", results.Diagnostic{Path: "/workspace/link/../clean.sh"}, script, 3)
	if err != nil || finding.Path != sourcePath(file, root) || string(finding.Content) != "VALUE=one\n" {
		t.Fatalf("symlink mapping=%+v error=%v", finding, err)
	}
	if err := os.WriteFile(file, []byte("VALUE=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := sourceRunner.sourceFiles(t.Context(), script, requestedSourceGrants(t, script.Config))
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
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "shared-sources")
	file := fixturefiles.Write(t, shared, "shared.sh", "SHARED=two\n")
	alias := filepath.Join(root, "shared")
	if err := os.Symlink(shared, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(shared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(shared, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(shared, "sub"), link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, directory, grant string }{
		{"resolved working directory", resolved, alias},
		{"aliased working directory", alias, alias},
		{"symlink before parent", resolved, link + string(os.PathSeparator) + ".."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := (config.RuleSet{}).Parse(fmt.Appendf(nil, "tools: {shellcheck: {config: {external-sources: true, source-path: [%s, %s, %s]}}}", strconv.Quote(file), strconv.Quote(filepath.Join(root, "missing")), strconv.Quote(tc.grant)))
			if err != nil {
				t.Fatal(err)
			}
			var inputs []string
			reader := inputfiles.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
			script := tools.Script{Path: "ci.yml", Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: reader}, Execution: tools.Execution{Directory: tc.directory, DirectoryKnown: true}, Key: &workflowlang.Pos{Line: 1}, Value: &workflowlang.Pos{Line: 1}}
			sources, err := sourceRunner.sourceFiles(t.Context(), script, requestedSourceGrants(t, script.Config))
			if err != nil {
				t.Fatal(err)
			}
			defer sources.close()
			input := checkinputs.Input{Options: []byte("{}"), Scripts: []checkinputs.Script{{Runtime: &scriptmodel.Execution{Directory: tc.directory, DirectoryKnown: true}}}}
			sources.configure(&input)
			if input.Scripts[0].Runtime.Directory != "/sources/2" || len(input.Files) != 2 {
				t.Fatalf("working directory not granted: %s", input.Scripts[0].Runtime.Directory)
			}
			for _, host := range []string{tc.grant, resolved} {
				if !slices.Contains(input.SourceRoots, checkinputs.SourceRoot{Host: filepath.ToSlash(host), Guest: "/sources/2"}) {
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
			dependencies := []string{sourcePath(file, shared)}
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
			finding, err := sources.finding("shellcheck", results.Diagnostic{Path: "/sources/2/shared.sh"}, script, 0)
			if err != nil || finding.Path != filepath.Join(resolved, "shared.sh") || string(finding.Content) != "SHARED=two\n" {
				t.Fatalf("source mapping=%+v error=%v", finding, err)
			}
		})
	}
}

func TestPluginWorkingDirectoryCannotGrantOutsideWorkspace(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	cfg, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {config: {external-sources: true}}}"))
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: inputfiles.New(t.Context(), nil, nil)}, Execution: tools.Execution{Directory: outside, DirectoryKnown: true}}
	if _, err := sourceRunner.sourceFiles(context.Background(), script, requestedSourceGrants(t, script.Config)); err == nil || !strings.Contains(err.Error(), "outside the configured read-access directories") {
		t.Fatalf("outside cwd granted: %v", err)
	}
	cfg, err = (config.RuleSet{}).Parse(fmt.Appendf(nil, "tools: {shellcheck: {config: {external-sources: true, source-path: [%s]}}}", strconv.Quote(outside)))
	if err != nil {
		t.Fatal(err)
	}
	script.Config = cfg
	if files, err := sourceRunner.sourceFiles(t.Context(), script, requestedSourceGrants(t, script.Config)); err == nil || files != nil || !strings.Contains(err.Error(), "outside workspace root") {
		t.Fatalf("configured outside cwd granted: files=%v error=%v", files, err)
	}
}

// requestedSourceGrants requests the workspace and every directory in
// tools.shellcheck.config.source-path, as a guest does.
func requestedSourceGrants(t *testing.T, cfg *config.Config) []checkinputs.ToolResourceRequest {
	t.Helper()
	resources := []checkinputs.ToolResourceRequest{{Kind: checkinputs.ToolWorkspace}}
	settings, err := sourceRunner.options(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range settings.ReadAccess.Directories {
		resources = append(resources, checkinputs.ToolResourceRequest{Kind: checkinputs.ToolDirectory, Path: path})
	}
	return resources
}

func TestPluginWorkingDirectoryUsesMostSpecificGrant(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {config: {external-sources: true}}}"))
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: root, Reader: inputfiles.New(t.Context(), nil, nil)}, Execution: tools.Execution{Directory: nested, DirectoryKnown: true}}
	files, err := sourceRunner.sourceFiles(t.Context(), script, []checkinputs.ToolResourceRequest{{Kind: checkinputs.ToolWorkspace}, {Kind: checkinputs.ToolDirectory, Path: nested}})
	if err != nil {
		t.Fatal(err)
	}
	defer files.close()
	if files.directory != "/sources/0" {
		t.Fatalf("directory=%s", files.directory)
	}
}

func TestPluginRequestsCannotExpandHostSourceAuthority(t *testing.T) {
	t.Parallel()
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
		{"repository external root", "{source-path: [" + strconv.Quote(outside) + "]}", outside, false},
		{"repository symlink root", "{source-path: [" + strconv.Quote(escape) + "]}", escape, false},
		{"denial overrides configured paths", "{external-sources: false, source-path: [" + strconv.Quote(outside) + "]}", outside, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {config: " + tc.config + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			var recorded []string
			script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: workspace, Reader: inputfiles.New(t.Context(), nil, func(name string) { recorded = append(recorded, name) })}, Execution: tools.Execution{Directory: workspace, DirectoryKnown: true}}
			resources := []checkinputs.ToolResourceRequest{{Kind: checkinputs.ToolWorkspace}, {Kind: checkinputs.ToolDirectory, Path: tc.requested}}
			files, err := sourceRunner.sourceFiles(t.Context(), script, resources)
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

func TestMissingPluginSourcePathCannotGrantEscape(t *testing.T) {
	t.Parallel()
	workspace, outside := t.TempDir(), t.TempDir()
	fixturefiles.Write(t, outside, "secret.txt", "private-host-marker")
	escape := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(escape, "missing")
	cfg, err := (config.RuleSet{}).Parse(fmt.Appendf(nil, "tools: {shellcheck: {config: {source-path: [%s]}}}", strconv.Quote(missing)))
	if err != nil {
		t.Fatal(err)
	}
	script := tools.Script{Config: cfg, Scope: &tools.Scope{Workspace: workspace, Reader: inputfiles.New(t.Context(), nil, nil)}, Execution: tools.Execution{Directory: workspace, DirectoryKnown: true}}
	files, err := sourceRunner.sourceFiles(t.Context(), script, requestedSourceGrants(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer files.close()
	if len(files.mounts) != 1 {
		t.Fatalf("missing path granted: %d mounts", len(files.mounts))
	}
	content, err := fs.ReadFile(files.mounts[0].files, "escape/secret.txt")
	if !errors.Is(err, fs.ErrPermission) || len(content) != 0 {
		t.Fatalf("escape read: content=%q error=%v", content, err)
	}
}

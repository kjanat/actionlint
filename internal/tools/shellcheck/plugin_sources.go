package shellcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools"
)

type pluginSourceMount struct {
	host, guest string
	files       *fsread.SnapshotFS
}

type pluginSourceFiles struct {
	mounts    []pluginSourceMount
	paths     []string
	directory string
}

type pluginSourceRoot struct {
	Host  string `json:"host"`
	Guest string `json:"guest"`
}

func (r documentPluginRunner) sourceFiles(ctx context.Context, script tools.Script) (*pluginSourceFiles, error) {
	files := &pluginSourceFiles{}
	budget := &fsread.SnapshotBudget{}
	if script.Config == nil || script.Scope == nil || !script.Execution.DirectoryKnown {
		return files, nil
	}
	inline := script.Config.Tools.Shellcheck.Config.Inline()
	if inline == nil || inline.ExternalSources != nil && !*inline.ExternalSources || inline.ExternalSources == nil && len(inline.SourcePath) == 0 {
		return files, nil
	}
	if len(inline.SourcePath) > 32 {
		return nil, errors.New("ShellCheck source access exceeds 32 search directories")
	}
	add := func(host, guest string) error {
		host, err := filepath.Abs(host)
		if err != nil {
			return err
		}
		snapshot, err := fsread.NewSnapshot(ctx, host, script.Scope.Reader, budget)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				script.Scope.Reader.Record(host)
				return nil
			}
			return fmt.Errorf("ShellCheck source directory %q: %w", host, err)
		}
		files.mounts = append(files.mounts, pluginSourceMount{host: host, guest: guest, files: snapshot})
		return nil
	}
	workspace := script.Scope.Workspace
	if workspace == "" {
		workspace = script.Scope.AnalysisDir
	}
	if workspace == "" {
		return nil, errors.New("ShellCheck source access requires a workspace root")
	}
	if err := add(workspace, "/workspace"); err != nil {
		return nil, err
	}
	run := &shellcheckRun{cfg: script.Config, actionPath: script.Scope.ActionPath, paths: runPaths{analysis: script.Scope.AnalysisDir, workspace: script.Scope.Workspace}}
	for i, directory := range inline.SourcePath {
		directory, err := run.pathContext().Expand(directory)
		if err != nil {
			return nil, errors.Join(err, files.close())
		}
		if directory == "SCRIPTDIR" || strings.HasPrefix(directory, "SCRIPTDIR/") {
			files.paths = append(files.paths, directory)
			continue
		}
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(script.Execution.Directory, directory)
		}
		guest := fmt.Sprintf("/sources/%d", i)
		if err := add(directory, guest); err != nil {
			return nil, errors.Join(err, files.close())
		}
		files.paths = append(files.paths, guest)
	}
	for _, mount := range files.mounts {
		relative, err := filepath.Rel(mount.host, script.Execution.Directory)
		if err == nil && filepath.IsLocal(relative) {
			files.directory = path.Join(mount.guest, filepath.ToSlash(relative))
			break
		}
	}
	if files.directory == "" {
		return nil, errors.Join(errors.New("ShellCheck working directory is outside its source grants; add an explicit source-path directory"), files.close())
	}
	return files, nil
}

func (f *pluginSourceFiles) configure(input *analyzer.Input) error {
	if len(f.mounts) == 0 {
		return nil
	}
	var options documentPluginOptions
	if err := json.Unmarshal(input.Options, &options); err != nil {
		return err
	}
	options.ExternalSources, options.SourcePath = true, f.paths
	for _, mount := range f.mounts {
		options.SourceRoots = append(options.SourceRoots, pluginSourceRoot{Host: filepath.ToSlash(mount.host), Guest: mount.guest})
	}
	data, err := json.Marshal(options)
	if err != nil {
		return err
	}
	input.Options = data
	for _, mount := range f.mounts {
		input.Files = append(input.Files, analyzer.FileSystem{Path: mount.guest, FS: mount.files})
	}
	input.Scripts[0].Runtime.Directory = f.directory
	return nil
}

func (f *pluginSourceFiles) close() error {
	var failure error
	for _, mount := range f.mounts {
		failure = errors.Join(failure, mount.files.Close())
	}
	return failure
}

func (f *pluginSourceFiles) finding(finding diagnostic.Diagnostic, script tools.Script, index int) (tools.Finding, error) {
	result := tools.Finding{Diagnostic: finding, Script: index, Content: script.Content}
	if finding.Path == script.Path {
		return result, nil
	}
	for _, mount := range f.mounts {
		name, ok := strings.CutPrefix(finding.Path, mount.guest+"/")
		if !ok {
			continue
		}
		name, content, ok := mount.files.Content(name)
		if !ok {
			return tools.Finding{}, fmt.Errorf("ShellCheck reported unread source %q", finding.Path)
		}
		result.Path = filepath.Join(mount.host, filepath.FromSlash(name))
		result.Reference, result.Fixes = nil, nil
		result.Content, result.RawScript = content, true
		last := script.Value.Line
		for _, span := range script.Source.LineSpans() {
			last = max(last, span.YamlLine)
		}
		result.Suppression = &tools.Suppression{Path: script.Path, First: script.Key.Line, Last: last}
		return result, nil
	}
	return tools.Finding{}, fmt.Errorf("ShellCheck reported source outside its grants: %q", finding.Path)
}

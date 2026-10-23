package shellcheck

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools"
)

type pluginSourceMount struct {
	host, alias, guest string
	files              *fsread.SnapshotFS
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
	directory := sourcePath(script.Execution.Directory, "")
	run := &shellcheckRun{cfg: script.Config, actionPath: script.Scope.ActionPath, paths: runPaths{analysis: script.Scope.AnalysisDir, workspace: script.Scope.Workspace, reader: script.Scope.Reader}}
	add := func(host, guest string) error {
		if !filepath.IsAbs(host) {
			base, err := os.Getwd()
			if err != nil {
				return err
			}
			host = sourceName(host, base)
		}
		if err := run.recordSources([]string{host}, directory); err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(host)
		if err == nil {
			resolved, err = filepath.Abs(resolved)
		}
		var info fs.FileInfo
		if err == nil {
			info, err = os.Stat(resolved)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			return fmt.Errorf("ShellCheck source directory %q: %w", host, err)
		}
		if !info.IsDir() {
			return nil
		}
		snapshot, err := fsread.NewSnapshot(ctx, resolved, script.Scope.Reader, budget)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			return fmt.Errorf("ShellCheck source directory %q: %w", host, err)
		}
		files.mounts = append(files.mounts, pluginSourceMount{host: resolved, alias: host, guest: guest, files: snapshot})
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
	for i, source := range inline.SourcePath {
		source, err := run.pathContext().Expand(source)
		if err != nil {
			return nil, errors.Join(err, files.close())
		}
		if source == "SCRIPTDIR" || strings.HasPrefix(source, "SCRIPTDIR/") {
			files.paths = append(files.paths, source)
			continue
		}
		source = sourceName(source, directory)
		guest := fmt.Sprintf("/sources/%d", i)
		if err := add(source, guest); err != nil {
			return nil, errors.Join(err, files.close())
		}
		files.paths = append(files.paths, guest)
	}
	for _, mount := range files.mounts {
		relative, err := filepath.Rel(mount.host, directory)
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
		if mount.alias != mount.host {
			options.SourceRoots = append(options.SourceRoots, pluginSourceRoot{Host: filepath.ToSlash(mount.alias), Guest: mount.guest})
		}
	}
	slices.SortStableFunc(options.SourceRoots, func(a, b pluginSourceRoot) int {
		return cmp.Compare(len(b.Host), len(a.Host))
	})
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

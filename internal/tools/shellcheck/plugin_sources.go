package shellcheck

import (
	"cmp"
	"context"
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
	directory string
}

type sourceAuthority struct {
	alias, host string
	root        *os.Root
}

func closeSourceAuthorities(authorities []sourceAuthority) error {
	var failure error
	for _, authority := range authorities {
		if authority.root != nil {
			failure = errors.Join(failure, authority.root.Close())
		}
	}
	return failure
}

func sourceAuthorities(script tools.Script, run *shellcheckRun, workspace, directory string) ([]sourceAuthority, error) {
	if script.Config == nil {
		return nil, errors.New("plugin source access is not authorized by host configuration")
	}
	inline := script.Config.Tools.Shellcheck.Config.Inline()
	if inline == nil || inline.ExternalSources != nil && !*inline.ExternalSources || inline.ExternalSources == nil && len(inline.SourcePath) == 0 {
		return nil, errors.New("plugin source access is not authorized by host configuration")
	}
	if len(inline.SourcePath) > 32 {
		return nil, errors.New("ShellCheck source access exceeds 32 search directories")
	}
	names := []string{workspace}
	for _, name := range inline.SourcePath {
		expanded, err := run.pathContext().Expand(name)
		if err != nil {
			return nil, err
		}
		if expanded != "SCRIPTDIR" && !strings.HasPrefix(expanded, "SCRIPTDIR/") {
			names = append(names, sourceName(expanded, directory))
		}
	}
	var authorities []sourceAuthority
	for _, name := range names {
		if !filepath.IsAbs(name) {
			base, err := os.Getwd()
			if err != nil {
				return nil, errors.Join(err, closeSourceAuthorities(authorities))
			}
			name = sourceName(name, base)
		}
		resolved, err := filepath.EvalSymlinks(name)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			authorities = append(authorities, sourceAuthority{alias: name, host: name})
			continue
		}
		if err != nil {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		if !info.IsDir() {
			authorities = append(authorities, sourceAuthority{alias: name, host: resolved})
			continue
		}
		root, err := os.OpenRoot(resolved)
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		authorities = append(authorities, sourceAuthority{alias: name, host: resolved, root: root})
	}
	return authorities, nil
}

func (r documentPluginRunner) sourceFiles(ctx context.Context, script tools.Script, resources []analyzer.ToolResourceRequest) (result *pluginSourceFiles, failure error) {
	files := &pluginSourceFiles{}
	budget := &fsread.SnapshotBudget{}
	if len(resources) == 0 {
		return files, nil
	}
	if script.Scope == nil || !script.Execution.DirectoryKnown {
		return nil, errors.New("source access requires a known workspace and working directory")
	}
	if len(resources) > 33 {
		return nil, errors.New("ShellCheck source access exceeds 32 search directories")
	}
	directory := sourcePath(script.Execution.Directory, "")
	run := &shellcheckRun{cfg: script.Config, actionPath: script.Scope.ActionPath, paths: runPaths{analysis: script.Scope.AnalysisDir, workspace: script.Scope.Workspace, reader: script.Scope.Reader}}
	workspace := script.Scope.Workspace
	if workspace == "" {
		workspace = script.Scope.AnalysisDir
	}
	if workspace == "" {
		return nil, errors.New("ShellCheck source access requires a workspace root")
	}
	authorities, err := sourceAuthorities(script, run, workspace, directory)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeSourceAuthorities(authorities); err != nil {
			failure = errors.Join(failure, err)
			if result != nil {
				failure = errors.Join(failure, result.close())
				result = nil
			}
		}
	}()
	add := func(host, guest string) error {
		if !filepath.IsAbs(host) {
			base, err := os.Getwd()
			if err != nil {
				return err
			}
			host = sourceName(host, base)
		}
		resolved, resolveErr := filepath.EvalSymlinks(host)
		if resolveErr != nil && !errors.Is(resolveErr, fs.ErrNotExist) && !errors.Is(resolveErr, syscall.ENOTDIR) {
			return fmt.Errorf("ShellCheck source directory %q: %w", host, resolveErr)
		}
		var selected *sourceAuthority
		var relative string
		for i := range authorities {
			authority := &authorities[i]
			candidate := resolved
			if resolveErr != nil {
				candidate = host
			}
			rel, err := filepath.Rel(authority.host, candidate)
			if err == nil && filepath.IsLocal(rel) && (selected == nil || len(authority.host) > len(selected.host)) {
				selected, relative = authority, rel
			}
			if resolveErr != nil && host == authority.alias && selected == nil {
				selected, relative = authority, "."
			}
		}
		if selected == nil {
			return fmt.Errorf("plugin requested source directory %q outside host-authorized roots", host)
		}
		if err := run.recordSources([]string{host}, directory); err != nil {
			return err
		}
		if resolveErr != nil || selected.root == nil {
			return nil
		}
		info, err := selected.root.Stat(relative)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		snapshot, err := fsread.NewSnapshotAt(ctx, selected.root, relative, script.Scope.Reader, budget)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			return fmt.Errorf("ShellCheck source directory %q: %w", host, err)
		}
		files.mounts = append(files.mounts, pluginSourceMount{host: resolved, alias: host, guest: guest, files: snapshot})
		return nil
	}
	directoryIndex := 0
	for _, resource := range resources {
		if resource.Kind == analyzer.ToolWorkspace {
			if err := add(workspace, "/workspace"); err != nil {
				return nil, errors.Join(err, files.close())
			}
			continue
		}
		if resource.Kind != analyzer.ToolDirectory {
			return nil, errors.Join(errors.New("unknown source resource kind"), files.close())
		}
		source := resource.Path
		source, err := run.pathContext().Expand(source)
		if err != nil {
			return nil, errors.Join(err, files.close())
		}
		if source == "SCRIPTDIR" || strings.HasPrefix(source, "SCRIPTDIR/") {
			continue
		}
		source = sourceName(source, directory)
		guest := fmt.Sprintf("/sources/%d", directoryIndex)
		directoryIndex++
		if err := add(source, guest); err != nil {
			return nil, errors.Join(err, files.close())
		}
	}
	matchedRootLength := -1
	for _, mount := range files.mounts {
		relative, err := filepath.Rel(mount.host, directory)
		if err == nil && filepath.IsLocal(relative) && len(mount.host) > matchedRootLength {
			files.directory = path.Join(mount.guest, filepath.ToSlash(relative))
			matchedRootLength = len(mount.host)
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
	for _, mount := range f.mounts {
		input.SourceRoots = append(input.SourceRoots, analyzer.SourceRoot{Host: filepath.ToSlash(mount.host), Guest: mount.guest})
		if mount.alias != mount.host {
			input.SourceRoots = append(input.SourceRoots, analyzer.SourceRoot{Host: filepath.ToSlash(mount.alias), Guest: mount.guest})
		}
	}
	slices.SortStableFunc(input.SourceRoots, func(a, b analyzer.SourceRoot) int {
		return cmp.Compare(len(b.Host), len(a.Host))
	})
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

package managed

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

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type pluginSourceMount struct {
	host, alias, guest string
	files              *files.BoundedFS
}

type pluginSourceFiles struct {
	mounts    []pluginSourceMount
	directory string
}

type sourceAuthority struct {
	alias, host string
	root        *os.Root
}

func (a sourceAuthority) checkLexical(name string) error {
	relative, err := filepath.Rel(a.alias, name)
	if err != nil || !filepath.IsLocal(relative) {
		relative, err = filepath.Rel(a.host, name)
	}
	if err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("plugin source directory %q is outside workspace root %q", name, a.host)
	}
	return nil
}

func resolveSourceDirectory(name string) (string, bool, error) {
	resolved, err := filepath.EvalSymlinks(name)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return name, false, nil
	}
	return resolved, err == nil, err
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

func sourceAuthorities(script scriptanalysis.Unit, settings integrationconfig.ResolvedSettings, paths configuration.PathContext, workspace, directory string) ([]sourceAuthority, error) {
	if script.Config() == nil {
		return nil, errors.New("plugin source access is not authorized by host configuration")
	}
	inline := settings.ReadAccess
	if inline.Enabled != nil && !*inline.Enabled || inline.Enabled == nil && len(inline.Directories) == 0 {
		return nil, errors.New("plugin source access is not authorized by host configuration")
	}
	if len(inline.Directories) > 32 {
		return nil, errors.New("tool source access exceeds 32 search directories")
	}
	names := []string{workspace}
	for _, name := range inline.Directories {
		expanded, err := paths.Expand(name)
		if err != nil {
			return nil, err
		}
		names = append(names, sourceName(expanded, directory))
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
		if len(authorities) != 0 {
			if err := authorities[0].checkLexical(name); err != nil {
				return nil, errors.Join(err, closeSourceAuthorities(authorities))
			}
		}
		resolved, exists, err := resolveSourceDirectory(name)
		if err != nil {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		var relative string
		if len(authorities) != 0 {
			workspace := &authorities[0]
			relative, err = filepath.Rel(workspace.host, resolved)
			if !exists && (err != nil || !filepath.IsLocal(relative)) {
				relative, err = filepath.Rel(workspace.alias, name)
			}
			if err != nil || !filepath.IsLocal(relative) {
				return nil, errors.Join(fmt.Errorf("plugin source directory %q is outside workspace root %q", name, workspace.host), closeSourceAuthorities(authorities))
			}
		}
		if !exists {
			if len(authorities) == 0 {
				return nil, fmt.Errorf("plugin workspace root %q does not exist", name)
			}
			authorities = append(authorities, sourceAuthority{alias: name, host: name})
			continue
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
			if len(authorities) == 0 {
				return nil, fmt.Errorf("plugin workspace root %q is not a directory", name)
			}
			authorities = append(authorities, sourceAuthority{alias: name, host: resolved})
			continue
		}
		var root *os.Root
		if len(authorities) == 0 {
			root, err = os.OpenRoot(resolved)
		} else {
			root, err = authorities[0].root.OpenRoot(relative)
		}
		if err != nil && (len(authorities) == 0 || !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR)) {
			return nil, errors.Join(err, closeSourceAuthorities(authorities))
		}
		authorities = append(authorities, sourceAuthority{alias: name, host: resolved, root: root})
	}
	return authorities, nil
}

func (r documentRunner) sourceFiles(ctx context.Context, script scriptanalysis.Unit, resources []checkinputs.ToolResourceRequest) (result *pluginSourceFiles, failure error) {
	pluginFiles := &pluginSourceFiles{}
	budget := &files.SnapshotBudget{}
	if len(resources) == 0 {
		return pluginFiles, nil
	}
	workingDirectory, directoryKnown := script.WorkingDirectory()
	if script.Scope == nil || !directoryKnown {
		return nil, errors.New("source access requires a known workspace and working directory")
	}
	if len(resources) > 33 {
		return nil, errors.New("tool source access exceeds 32 search directories")
	}
	directory := sourcePath(workingDirectory, "")
	paths := backends.PathContext(script.Scope)
	settings, err := r.options(ctx, script.Config(), nil)
	if err != nil {
		return nil, err
	}
	workspace := script.Scope.Paths.Workspace
	if workspace == "" {
		workspace = script.Scope.Paths.Analysis
	}
	if workspace == "" {
		return nil, errors.New("tool source access requires a workspace root")
	}
	authorities, err := sourceAuthorities(script, settings, paths, workspace, directory)
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
		if err := authorities[0].checkLexical(host); err != nil {
			return err
		}
		resolved, exists, err := resolveSourceDirectory(host)
		if err != nil {
			return fmt.Errorf("tool source directory %q: %w", host, err)
		}
		var selected *sourceAuthority
		var relative string
		for i := range authorities {
			authority := &authorities[i]
			rel, err := filepath.Rel(authority.host, resolved)
			if err == nil && filepath.IsLocal(rel) && (selected == nil || len(authority.host) > len(selected.host)) {
				selected, relative = authority, rel
			}
			if !exists && host == authority.alias && selected == nil {
				selected, relative = authority, "."
			}
		}
		if selected == nil {
			return fmt.Errorf("plugin requested source directory %q outside host-authorized roots", host)
		}
		if err := backends.RecordSources(script.Scope.Reader, []string{host}, directory); err != nil {
			return err
		}
		if !exists || selected.root == nil {
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
		snapshot, err := files.NewSnapshotAt(ctx, selected.root, relative, script.Scope.Reader, budget)
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return fmt.Errorf("tool source directory %q: %w", host, err)
		}
		if snapshot != nil {
			pluginFiles.mounts = append(pluginFiles.mounts, pluginSourceMount{host: resolved, alias: host, guest: guest, files: snapshot})
		}
		return nil
	}
	directoryIndex := 0
	for _, resource := range resources {
		if resource.Kind == checkinputs.ToolWorkspace {
			if err := add(workspace, "/workspace"); err != nil {
				return nil, errors.Join(err, pluginFiles.close())
			}
			continue
		}
		if resource.Kind != checkinputs.ToolDirectory {
			return nil, errors.Join(errors.New("unknown source resource kind"), pluginFiles.close())
		}
		source := resource.Path
		source, err := paths.Expand(source)
		if err != nil {
			return nil, errors.Join(err, pluginFiles.close())
		}
		source = sourceName(source, directory)
		guest := fmt.Sprintf("/sources/%d", directoryIndex)
		directoryIndex++
		if err := add(source, guest); err != nil {
			return nil, errors.Join(err, pluginFiles.close())
		}
	}
	matchedRootLength := -1
	for _, mount := range pluginFiles.mounts {
		relative, err := filepath.Rel(mount.host, directory)
		if err == nil && filepath.IsLocal(relative) && len(mount.host) > matchedRootLength {
			pluginFiles.directory = path.Join(mount.guest, filepath.ToSlash(relative))
			matchedRootLength = len(mount.host)
		}
	}
	if pluginFiles.directory == "" {
		return nil, errors.Join(errors.New("tool working directory is outside the configured read-access directories"), pluginFiles.close())
	}
	return pluginFiles, nil
}

func (f *pluginSourceFiles) configure(input *checkinputs.AnalyzerInput) {
	if len(f.mounts) == 0 {
		return
	}
	for _, mount := range f.mounts {
		input.SourceRoots = append(input.SourceRoots, checkinputs.SourceRoot{Host: filepath.ToSlash(mount.host), Guest: mount.guest})
		if mount.alias != mount.host {
			input.SourceRoots = append(input.SourceRoots, checkinputs.SourceRoot{Host: filepath.ToSlash(mount.alias), Guest: mount.guest})
		}
	}
	slices.SortStableFunc(input.SourceRoots, func(a, b checkinputs.SourceRoot) int {
		return cmp.Compare(len(b.Host), len(a.Host))
	})
	for _, mount := range f.mounts {
		input.Files = append(input.Files, checkinputs.FileSystem{Path: mount.guest, FS: mount.files})
	}
	for _, scripts := range [][]checkinputs.Script{input.Scripts, input.Invoked} {
		for i := range scripts {
			if scripts[i].Runtime != nil {
				runtime := *scripts[i].Runtime
				runtime.Directory = f.directory
				scripts[i].Runtime = &runtime
			}
		}
	}
}

func (f *pluginSourceFiles) close() error {
	var failure error
	for _, mount := range f.mounts {
		failure = errors.Join(failure, mount.files.Close())
	}
	return failure
}

func (f *pluginSourceFiles) finding(name string, finding results.Diagnostic, script scriptanalysis.Unit, index int) (contract.ToolFinding, error) {
	result := contract.ToolFinding{Diagnostic: finding, Script: index, Source: results.DiagnosticSource{Content: script.Content}}
	if finding.Path == script.Path {
		return result, nil
	}
	if reference := finding.Reference; reference != nil && reference.Document != "" && reference.Revision != "" && reference.Node != "" && *reference == script.Reference {
		result.Path = script.Path
		return result, nil
	}
	for _, mount := range f.mounts {
		relative, ok := strings.CutPrefix(finding.Path, mount.guest+"/")
		if !ok {
			continue
		}
		relative, content, ok := mount.files.Content(relative)
		if !ok {
			return contract.ToolFinding{}, fmt.Errorf("tool reported unread source %q", finding.Path)
		}
		result.Path = filepath.Join(mount.host, filepath.FromSlash(relative))
		result.Reference, result.Fixes = nil, nil
		result.Source.Content, result.Source.RawScript = content, true
		result.Source.Unavailable = results.ReportedSourceUnavailable(name, result.Path, false)
		last := script.Value().Line
		if source := script.Source(); source != nil {
			for _, span := range sourcemap.LineSpans(source.Value, source.Spans) {
				last = max(last, span.Document.Line)
			}
		}
		result.Suppression = &results.SuppressionOrigin{Path: script.Path, First: script.Key().Line, Last: last}
		return result, nil
	}
	return contract.ToolFinding{}, fmt.Errorf("tool reported source outside its grants: %q", finding.Path)
}

// sourceName joins a relative path to directory without cleaning it, as a
// tool spells the file it read.
func sourceName(name, directory string) string {
	if !filepath.IsAbs(name) {
		name = directory + string(os.PathSeparator) + name
	}
	return name
}

// sourcePath is a path's absolute form with symlinks resolved.
func sourcePath(name, directory string) string {
	name = sourceName(name, directory)
	// Resolve symlinks before cleaning parent components.
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		name = resolved
	}
	if absolute, err := filepath.Abs(name); err == nil {
		return absolute
	}
	return name
}

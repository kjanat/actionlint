package backend

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools"
)

// reportedFile is the display copy of a file an engine reported a finding
// in, read once per check.
type reportedFile struct {
	content    []byte
	readFailed bool
}

// checkGranted runs one check on a backend that is not a native process.
// When the engine requested the directory resource, the runner opens the
// working directory read-only inside the workspace for the call and reads
// the display copies of reported sources through the same grant. The engine
// and the finding then see the same bytes, the scope records every file
// read, and no reported name reaches a file outside the workspace. A
// directory that does not exist gets no grant and is not sent.
//
// Reported sources of such a backend are never reread by path: without a
// grant they have no display copy.
func (r runner) checkGranted(ctx context.Context, c check) outcome {
	request := c.request
	result := outcome{copies: map[string]reportedFile{}}
	var grant *fsread.SnapshotFS
	if r.directory && request.Directory != "" {
		var err error
		if grant, err = directoryGrant(ctx, c.scope, request.Directory); err != nil {
			result.err = fmt.Errorf("tool %s %w", r.descriptor.Name, err)
			return result
		}
		if grant == nil {
			request.Directory = ""
		} else {
			request.DirectoryFS = grant
		}
	}
	result.diagnostics, result.err = r.backend.Check(ctx, request)
	result.directory = request.Directory
	if grant == nil {
		return result
	}
	for _, d := range result.diagnostics {
		if _, read := result.copies[d.Source]; d.Source == "" || read {
			continue
		}
		file := reportedFile{content: []byte{}, readFailed: true}
		if content, err := grant.ReadFile(d.Source); err == nil {
			file = reportedFile{content: content}
		} else {
			r.debug("Cannot read %s diagnostic source %q: %v", r.descriptor.Name, d.Source, err)
		}
		result.copies[d.Source] = file
	}
	if err := grant.Close(); err != nil {
		result.err = errors.Join(result.err, fmt.Errorf("tool %s working directory: %w", r.descriptor.Name, err))
	}
	return result
}

// directoryGrant opens directory read-only through the workspace root of
// scope, so neither the path nor a symlink below it leaves the workspace.
// Reads through the grant are recorded with the scope's reader. A missing
// directory gets no grant; one outside the workspace fails the check.
func directoryGrant(ctx context.Context, scope *tools.Scope, directory string) (*fsread.SnapshotFS, error) {
	if scope == nil || scope.Workspace == "" && scope.AnalysisDir == "" {
		return nil, errors.New("directory access requires a workspace root")
	}
	workspace := scope.Workspace
	if workspace == "" {
		workspace = scope.AnalysisDir
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(workspace, directory)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("working directory %q is outside workspace root %q", directory, workspace)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	files := scope.Reader
	if files == nil {
		files = fsread.New(ctx, nil, nil)
	}
	grant, err := fsread.NewSnapshotAt(ctx, root, relative, files, nil)
	closeErr := root.Close()
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, closeErr
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("working directory %q: %w", directory, err), closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, grant.Close())
	}
	return grant, nil
}

// reader is the script scope's recording reader, or nil.
func reader(script tools.Script) *fsread.Reader {
	if script.Scope == nil {
		return nil
	}
	return script.Scope.Reader
}

// recordSources records the files an engine read so watchers re-run the
// check when they change.
func (r runner) recordSources(script tools.Script, paths []string, directory string) error {
	if err := RecordSources(reader(script), paths, directory); err != nil {
		return fmt.Errorf("tool %s %w", r.descriptor.Name, err)
	}
	return nil
}

// RecordSources records files a tool read, relative to directory, with
// files, including symlinks along their paths. A nil reader records nothing.
func RecordSources(files *fsread.Reader, paths []string, directory string) error {
	for _, path := range paths {
		if path == "" || path == "-" || strings.ContainsRune(path, 0) {
			return fmt.Errorf("reported invalid source path %q", path)
		}
		if files == nil {
			continue
		}
		name := sourceName(path, directory)
		for end := 1; end <= len(name); end++ {
			if end != len(name) && !os.IsPathSeparator(name[end]) {
				continue
			}
			prefix := name[:end]
			info, err := os.Lstat(prefix)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			parent, base := filepath.Split(prefix)
			if resolved, err := filepath.EvalSymlinks(parent); err == nil {
				files.Record(filepath.Join(resolved, base))
			}
		}
		// The engine already read it. Keep the dependency even if it disappeared.
		files.Record(sourceInventoryPath(name, directory))
	}
	return nil
}

// reportedFinding is a finding in a file the engine read besides the
// snippet. Its positions are that file's own and its snippet is a display
// copy, never evidence of the revision the engine analyzed. Suppression
// comments of the script still apply. A check with copies takes the display
// copy from them; otherwise the copy is reread by path, as a native process
// read the file by path itself.
func (r runner) reportedFinding(index int, script tools.Script, d analyzer.ScriptDiagnostic, result outcome, files map[string]reportedFile) tools.Finding {
	directory := result.directory
	path := sourcePath(d.Source, directory)
	reported := sourceInventoryPath(sourceName(d.Source, directory), directory)
	file, found := files[path]
	if result.copies != nil {
		file, found = result.copies[d.Source]
		if !found {
			file = reportedFile{content: []byte{}, readFailed: true}
		}
	} else if !found {
		file.content, file.readFailed = []byte{}, true
		if scope := reader(script); scope != nil {
			content, err := scope.Mapped(func(string) string { return reported }).ReadFile(path)
			if err == nil {
				file.content, file.readFailed = content, false
			} else {
				r.debug("Cannot read %s diagnostic source %q: %v", r.descriptor.Name, path, err)
			}
		}
		files[path] = file
	}
	if scope := reader(script); scope != nil && (result.copies == nil || directory != "") {
		// A cached or failed display reread does not erase the reported alias.
		// Without a directory the name anchors nowhere and records nothing.
		scope.Record(reported)
	}
	finding := tools.Finding{
		Script: index, Content: file.content, RawScript: true,
		UnavailableSource: findingmodel.ReportedSourceUnavailable(r.descriptor.Name, sourceName(d.Source, directory), file.readFailed),
	}
	finding.Rule, finding.Path, finding.Code, finding.Severity, finding.DocumentationURL = r.descriptor.Name, path, d.Code, string(r.severity(d.Severity)), d.URL
	finding.Message = r.message(d, d.Location.Row, d.Location.Column)
	finding.Start = diagnostic.Position{Line: d.Location.Row, Column: d.Location.Column}
	if d.EndLocation.Row >= d.Location.Row && d.EndLocation.Column > 0 {
		finding.End = diagnostic.Position{Line: d.EndLocation.Row, Column: d.EndLocation.Column}
	}
	if key := script.Key; key != nil {
		origin := &tools.Suppression{Path: script.Path, First: key.Line, Last: key.Line}
		if value := script.Value; value != nil {
			origin.Last = max(origin.Last, value.Line)
		}
		for span := range script.Source.Spans() {
			origin.Last = max(origin.Last, span.YamlLine)
		}
		finding.Suppression = origin
	}
	return finding
}

// sourceName joins a relative reported path to the engine's directory
// without cleaning it, as the engine spelled the file it read.
func sourceName(name, directory string) string {
	if !filepath.IsAbs(name) {
		name = directory + string(os.PathSeparator) + name
	}
	return name
}

// sourcePath is the reported file's absolute path with symlinks resolved.
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

func sourceInventoryPath(name, directory string) string {
	for component := range strings.SplitSeq(filepath.ToSlash(name), "/") {
		if component == ".." {
			// Windows host APIs clean parents before following symlinks. Preserve
			// the resolved host-read spelling for these paths so watchers track the
			// file the engine read, rather than the lexically cleaned wrong file.
			return sourcePath(name, directory)
		}
	}
	return name
}

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// comments of the script still apply.
func (r runner) reportedFinding(index int, script tools.Script, d analyzer.ScriptDiagnostic, directory string, files map[string]reportedFile) tools.Finding {
	path := sourcePath(d.Source, directory)
	reported := sourceInventoryPath(sourceName(d.Source, directory), directory)
	file, found := files[path]
	if !found {
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
	if scope := reader(script); scope != nil {
		// A cached or failed display reread does not erase the reported alias.
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

package component

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"github.com/kjanat/wacogo"
)

// scriptDirectoryMount is the guest path of a granted working directory.
const scriptDirectoryMount = "/directory"

// liftScriptCapabilities reads the capabilities of a script-engine 0.3.0
// descriptor into d.
func liftScriptCapabilities(value wacogo.Val, d *analyzer.ScriptEngineDescriptor) error {
	r, err := liftRecord(value, "script capabilities")
	if err != nil {
		return err
	}
	if d.Fixes, err = liftBool(r, "fixes"); err != nil {
		return err
	}
	if d.ReportedSources, err = liftBool(r, "reported-sources"); err != nil {
		return err
	}
	list, err := liftList(r.Field("resources"), "script resources")
	if err != nil {
		return err
	}
	for i := range list.Len() {
		kind, err := liftRunnerEnum(list.Get(i), script_engine_03_resource_kind)
		if err != nil {
			return err
		}
		resource := analyzer.ToolResourceKind(kind)
		if slices.Contains(d.Resources, resource) {
			return fmt.Errorf("script descriptor requests resource %q twice", kind)
		}
		d.Resources = append(d.Resources, resource)
	}
	if d.Fixes {
		d.Rule.FixAvailability = "suggested"
	}
	return nil
}

// scriptGrant is the filesystem access the host grants for one check.
type scriptGrant struct {
	files     []analyzer.FileSystem
	directory string
	snapshot  *fsread.SnapshotFS
}

func (g scriptGrant) close() error {
	if g.snapshot == nil {
		return nil
	}
	return g.snapshot.Close()
}

// grantScriptResources mounts the script's working directory read-only when
// the engine requested it and the request names one. Without a grant the
// engine sees no files.
func (p *Plugin) grantScriptResources(ctx context.Context, request analyzer.ScriptRequest) (scriptGrant, error) {
	if request.Directory == "" || !slices.Contains(p.scriptDescriptor.Resources, analyzer.ToolDirectory) {
		return scriptGrant{}, nil
	}
	directory, err := filepath.Abs(request.Directory)
	if err != nil {
		return scriptGrant{}, err
	}
	// The runner records the files findings name; reads alone record nothing.
	snapshot, err := fsread.NewSnapshot(ctx, directory, fsread.New(ctx, nil, nil), nil)
	if err != nil {
		return scriptGrant{}, fmt.Errorf("script engine %s working directory: %w", p.scriptDescriptor.ID, err)
	}
	return scriptGrant{files: []analyzer.FileSystem{{Path: scriptDirectoryMount, FS: snapshot}}, directory: scriptDirectoryMount, snapshot: snapshot}, nil
}

// lowerScriptRequest03 builds a script-engine 0.3.0 request.
func lowerScriptRequest03(request analyzer.ScriptRequest, options string, grant scriptGrant) *wacogo.ValRecord {
	excluded := make([]wacogo.Val, len(request.Excluded))
	for i, code := range request.Excluded {
		excluded[i] = wacogo.ValString(code)
	}
	directory := wacogo.ValOptionNone()
	if grant.directory != "" {
		directory = wacogo.ValOptionSome(wacogo.ValString(grant.directory))
	}
	return wacogo.NewValRecord(field("source", wacogo.ValString(request.Source)),
		field("filename", wacogo.ValString(request.Filename)), field("config", wacogo.ValString(options)),
		field("excluded", wacogo.NewValListOf(excluded...)), field("directory", directory))
}

// validExcluded rejects excluded codes the request cannot carry.
func validExcluded(codes []string) bool {
	for _, code := range codes {
		if code == "" || strings.ContainsRune(code, 0) || !utf8.ValidString(code) {
			return false
		}
	}
	return true
}

// liftScriptDiagnostic03 lifts a script-engine 0.3.0 diagnostic, including
// its reported source and fixes.
func liftScriptDiagnostic03(value wacogo.Val) (analyzer.ScriptDiagnostic, error) {
	r, err := liftRecord(value, "script diagnostic")
	if err != nil {
		return analyzer.ScriptDiagnostic{}, err
	}
	d, err := liftScriptDiagnosticFields(r)
	if err != nil {
		return d, err
	}
	if d.Severity, err = liftString(r, "severity"); err != nil {
		return d, err
	}
	source, err := liftScriptOptionalString(r.Field("source"), "diagnostic source")
	if err != nil {
		return d, err
	}
	if source != nil {
		d.Source = *source
		if !validReportedSource(d.Source) {
			return d, fmt.Errorf("script engine reported invalid source %q", d.Source)
		}
	}
	fixes, err := liftList(r.Field("fixes"), "script fixes")
	if err != nil {
		return d, err
	}
	for i := range fixes.Len() {
		fix, err := liftScriptFix(fixes.Get(i))
		if err != nil {
			return d, err
		}
		d.Fixes = append(d.Fixes, fix)
	}
	return d, nil
}

// validReportedSource accepts a relative slash-separated path below the
// request directory. Backslashes and colons would let a Windows host read
// the name as a different path than the guest opened.
func validReportedSource(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:")
}

func liftScriptFix(value wacogo.Val) (analyzer.ScriptFix, error) {
	r, err := liftRecord(value, "script fix")
	if err != nil {
		return analyzer.ScriptFix{}, err
	}
	var fix analyzer.ScriptFix
	if fix.Description, err = liftString(r, "description"); err != nil {
		return fix, err
	}
	list, err := liftList(r.Field("replacements"), "script replacements")
	if err != nil {
		return fix, err
	}
	if list.Len() == 0 {
		return fix, errors.New("script engine returned a fix without replacements")
	}
	for i := range list.Len() {
		replacement, err := liftRecord(list.Get(i), "script replacement")
		if err != nil {
			return fix, err
		}
		start, err := liftScriptPosition(replacement.Field("location"))
		if err != nil {
			return fix, err
		}
		end, err := liftScriptPosition(replacement.Field("end-location"))
		if err != nil {
			return fix, err
		}
		text, err := liftString(replacement, "text")
		if err != nil {
			return fix, err
		}
		fix.Replacements = append(fix.Replacements, analyzer.ScriptReplacement{Row: start.Row, Column: start.Column, EndRow: end.Row, EndColumn: end.Column, Text: text})
	}
	return fix, nil
}

// checkScriptDiagnostic03 validates the 0.3.0 parts of a diagnostic against
// the engine's capabilities and this check's grant, and converts fix
// positions to decoded-source rows as the diagnostic's own.
func (p *Plugin) checkScriptDiagnostic03(d *analyzer.ScriptDiagnostic, grant scriptGrant, lines []int, starts []analyzer.ScriptPosition) error {
	descriptor := p.scriptDescriptor
	if d.Severity == "" {
		return errors.New("script engine returned a diagnostic without a severity")
	}
	if d.Source != "" {
		switch {
		case !descriptor.ReportedSources:
			return fmt.Errorf("script engine %s reported a finding in %q but does not declare reported sources", descriptor.ID, d.Source)
		case grant.directory == "":
			return fmt.Errorf("script engine %s reported a finding in %q without a granted directory", descriptor.ID, d.Source)
		case len(d.Fixes) != 0:
			return fmt.Errorf("script engine %s returned fixes for reported source %q", descriptor.ID, d.Source)
		case d.Code == "" || d.Message == "" || !orderedScriptRange(d.Location, d.EndLocation):
			return errors.New("script engine returned an invalid diagnostic or source range")
		}
		return nil
	}
	if len(d.Fixes) != 0 && !descriptor.Fixes {
		return fmt.Errorf("script engine %s returned fixes but does not declare them", descriptor.ID)
	}
	for i := range d.Fixes {
		for j := range d.Fixes[i].Replacements {
			r := &d.Fixes[i].Replacements[j]
			start, end := analyzer.ScriptPosition{Row: r.Row, Column: r.Column}, analyzer.ScriptPosition{Row: r.EndRow, Column: r.EndColumn}
			if !validScriptPosition(lines, start) || !validScriptPosition(lines, end) || !orderedScriptRange(start, end) {
				return errors.New("script engine returned a fix with an invalid source range")
			}
			start, end = scriptSourcePosition(starts, start), scriptSourcePosition(starts, end)
			r.Row, r.Column, r.EndRow, r.EndColumn = start.Row, start.Column, end.Row, end.Column
		}
	}
	return nil
}

// orderedScriptRange reports whether a range has positive coordinates and
// does not end before it starts.
func orderedScriptRange(start, end analyzer.ScriptPosition) bool {
	return start.Row > 0 && start.Column > 0 && end.Row > 0 && end.Column > 0 &&
		(end.Row > start.Row || end.Row == start.Row && end.Column >= start.Column)
}

// scriptDiagnosticBytes counts the text a diagnostic carries towards the
// result limit.
func scriptDiagnosticBytes(d analyzer.ScriptDiagnostic) uint64 {
	total := uint64(0)
	for _, text := range []string{d.Filename, d.Code, d.Name, d.Message, d.Severity, d.URL, d.Source} {
		total += uint64(len(text))
	}
	for _, fix := range d.Fixes {
		total += uint64(len(fix.Description))
		for _, replacement := range fix.Replacements {
			total += uint64(len(replacement.Text))
		}
	}
	return total
}

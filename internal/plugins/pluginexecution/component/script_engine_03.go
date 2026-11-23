package component

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine/enginediagnostics"
	"github.com/kjanat/wacogo"
)

const scriptDirectoryMount = "/directory"

func liftScriptCapabilities(value wacogo.Val, d *scriptengine.Descriptor) error {
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
		resource := checkinputs.ToolResourceKind(kind)
		if slices.Contains(d.Resources, resource) {
			return fmt.Errorf("script descriptor requests resource %q twice", kind)
		}
		d.Resources = append(d.Resources, resource)
	}
	return nil
}

type scriptGrant struct {
	files     []checkinputs.FileSystem
	directory string
}

// grantScriptResources mounts the host's directory grant read-only when the
// engine requested the directory resource.
func grantScriptResources(request scriptengine.Request, d *scriptengine.Descriptor) scriptGrant {
	if request.DirectoryFS == nil || !slices.Contains(d.Resources, checkinputs.ToolDirectory) {
		return scriptGrant{}
	}
	return scriptGrant{files: []checkinputs.FileSystem{{Path: scriptDirectoryMount, FS: request.DirectoryFS}}, directory: scriptDirectoryMount}
}

type staticScriptCapabilities struct {
	Shells          []string                       `json:"script_shells"`
	Fixes           bool                           `json:"fixes"`
	ReportedSources bool                           `json:"reported_sources"`
	Resources       []checkinputs.ToolResourceKind `json:"resources"`
}

func readStaticScriptCapabilities03(declared staticScriptCapabilities, d *scriptengine.Descriptor) error {
	d.Fixes, d.ReportedSources = declared.Fixes, declared.ReportedSources
	for _, resource := range declared.Resources {
		if resource != checkinputs.ToolDirectory || slices.Contains(d.Resources, resource) {
			return fmt.Errorf("script descriptor requests invalid or duplicate resource %q", resource)
		}
		d.Resources = append(d.Resources, resource)
	}
	return nil
}

func lowerScriptRequest03(request scriptengine.Request, options string, grant scriptGrant) *wacogo.ValRecord {
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

func validExcluded(codes []string) bool {
	for _, code := range codes {
		if code == "" || strings.ContainsRune(code, 0) || !utf8.ValidString(code) {
			return false
		}
	}
	return true
}

func liftScriptDiagnostic03(value wacogo.Val) (scriptengine.Diagnostic, error) {
	r, err := liftRecord(value, "script diagnostic")
	if err != nil {
		return scriptengine.Diagnostic{}, err
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
		if !enginediagnostics.ValidReportedSource(d.Source) {
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

func liftScriptFix(value wacogo.Val) (scriptengine.Fix, error) {
	r, err := liftRecord(value, "script fix")
	if err != nil {
		return scriptengine.Fix{}, err
	}
	var fix scriptengine.Fix
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
		fix.Replacements = append(fix.Replacements, scriptengine.Replacement{Start: start, End: end, Text: text})
	}
	return fix, nil
}

// checkScriptDiagnostic03 leaves positions and fix ranges to enginediagnostics.
func checkScriptDiagnostic03(d *scriptengine.Diagnostic, descriptor *scriptengine.Descriptor, grant scriptGrant) error {
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
		case d.Code == "" || d.Message == "":
			return errors.New("script engine returned an invalid diagnostic")
		}
		return nil
	}
	if len(d.Fixes) != 0 && !descriptor.Fixes {
		return fmt.Errorf("script engine %s returned fixes but does not declare them", descriptor.ID)
	}
	return nil
}

func scriptDiagnosticBytes(d scriptengine.Diagnostic) uint64 {
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

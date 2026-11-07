package component

import (
	"errors"
	"fmt"
	"math"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/related"
	"github.com/kjanat/wacogo"
)

// WIT enum order comes from the generated canonical contract, not a second table.
var inputKinds = func() []rulemeta.InputKind {
	facts := make([]rulemeta.InputKind, len(canonical_input_kind))
	for i, name := range canonical_input_kind {
		facts[i] = rulemeta.InputKind(name)
	}
	return facts
}()

var inputKindDiscriminants = func() map[rulemeta.InputKind]uint32 {
	values := make(map[rulemeta.InputKind]uint32, len(inputKinds))
	for i, fact := range inputKinds {
		values[fact] = uint32(i)
	}
	return values
}()

func validInputKindDiscriminant(discriminant uint32) bool {
	return uint64(discriminant) < uint64(len(inputKinds))
}

var keyStateDiscriminants = func() map[workflowfacts.KeyState]uint32 {
	values := make(map[workflowfacts.KeyState]uint32, len(canonical_key_state))
	for i, name := range canonical_key_state {
		values[workflowfacts.KeyState(name)] = uint32(i)
	}
	return values
}()

func field(name string, value wacogo.Val) wacogo.Field {
	return wacogo.Field{Name: name, Val: value}
}

func lowerU32(n int) wacogo.ValU32 {
	if n < 0 {
		return 0
	}
	if uint64(n) > math.MaxUint32 {
		return math.MaxUint32
	}
	return wacogo.ValU32(uint32(n))
}

func lowerPosition(p sourcepos.Position) *wacogo.ValRecord {
	return wacogo.NewValRecord(field("line", lowerU32(p.Line)), field("column", lowerU32(p.Column)))
}

func lowerInputKinds(facts []rulemeta.InputKind) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(facts))
	for _, fact := range facts {
		if discriminant, ok := inputKindDiscriminants[fact]; ok {
			values = append(values, wacogo.NewValEnum(discriminant))
		}
	}
	return wacogo.NewValListOf(values...)
}

func lowerSpan(span scriptmodel.Span) *wacogo.ValRecord {
	documentEnd := wacogo.ValOptionNone()
	if span.DocumentEnd.Line != 0 {
		documentEnd = wacogo.ValOptionSome(lowerPosition(span.DocumentEnd))
	}
	return wacogo.NewValRecord(
		field("line", lowerU32(span.Line)), field("start", lowerU32(span.Start)), field("end", lowerU32(span.End)),
		field("document", lowerPosition(span.Document)), field("document-end", documentEnd),
	)
}

func lowerScript(script checkinputs.Script) *wacogo.ValRecord {
	spans := make([]wacogo.Val, 0, len(script.Spans))
	for _, span := range script.Spans {
		spans = append(spans, lowerSpan(span))
	}
	occurrence := wacogo.ValOptionNone()
	if script.Occurrence.ID != "" {
		occurrence = wacogo.ValOptionSome(wacogo.ValString(script.Occurrence.ID))
	}
	runtime := wacogo.ValOptionNone()
	if script.Runtime != nil {
		runtime = wacogo.ValOptionSome(wacogo.NewValRecord(
			field("shell", wacogo.ValString(script.Runtime.Shell)), field("shell-known", wacogo.ValBool(script.Runtime.ShellKnown)),
			field("shell-implicit", wacogo.ValBool(script.Runtime.ShellImplicit)), field("directory", wacogo.ValString(script.Runtime.Directory)),
			field("directory-known", wacogo.ValBool(script.Runtime.DirectoryKnown)),
		))
	}
	return wacogo.NewValRecord(
		field("reference", lowerReference(script.Reference)), field("job-id", wacogo.ValString(script.JobID)), field("step-id", wacogo.ValString(script.StepID)),
		field("text", wacogo.ValString(script.Text)), field("shell", wacogo.ValString(script.Shell)),
		field("start", lowerPosition(script.Start)), field("path", wacogo.ValString(script.Path)),
		field("spans", wacogo.NewValListOf(spans...)), field("occurrence", occurrence), field("runtime", runtime),
	)
}

func lowerScripts(scripts []checkinputs.Script) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(scripts))
	for _, script := range scripts {
		values = append(values, lowerScript(script))
	}
	return wacogo.NewValListOf(values...)
}

func lowerMappings(mappings []workflowfacts.Mapping) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(mappings))
	for _, mapping := range mappings {
		entries := make([]wacogo.Val, 0, len(mapping.Entries))
		for _, entry := range mapping.Entries {
			entries = append(entries, wacogo.NewValRecord(
				field("name", wacogo.ValString(entry.Name)), field("state", wacogo.NewValEnum(keyStateDiscriminants[entry.State])),
				field("source", wacogo.ValString(entry.Source)), field("evaluated", wacogo.ValBool(entry.Evaluated)),
				field("start", lowerPosition(entry.Start)),
			))
		}
		values = append(values, wacogo.NewValRecord(
			field("reference", lowerReference(mapping.Reference)), field("scope", wacogo.ValString(mapping.Scope)), field("complete", wacogo.ValBool(mapping.Complete)),
			field("entries", wacogo.NewValListOf(entries...)),
		))
	}
	return wacogo.NewValListOf(values...)
}

func lowerInput(input checkinputs.Input) *wacogo.ValRecord {
	selected := wacogo.ValOptionNone()
	if input.SelectedScripts != nil {
		ids := make([]wacogo.Val, 0, len(input.SelectedScripts))
		for _, id := range input.SelectedScripts {
			ids = append(ids, wacogo.ValString(id))
		}
		selected = wacogo.ValOptionSome(wacogo.NewValListOf(ids...))
	}
	inputs := make([]wacogo.Val, 0, len(input.ActionInputs))
	for _, value := range input.ActionInputs {
		inputs = append(inputs, wacogo.NewValRecord(field("script", lowerScript(value.Script)),
			field("action", wacogo.ValString(value.Site.Action)), field("input", wacogo.ValString(value.Site.Input)), field("language", wacogo.ValString(value.Site.Language))))
	}
	return wacogo.NewValRecord(
		field("reference", lowerReference(input.Reference)), field("path", wacogo.ValString(input.Path)), field("content", wacogo.ValString(input.Content)),
		field("kind", wacogo.ValString(input.Kind)), field("parse-status", wacogo.ValString(input.ParseStatus)),
		field("available-inputs", lowerInputKinds(input.AvailableInputs)), field("options", wacogo.ValString(string(input.Options))),
		field("scripts", lowerScripts(input.Scripts)), field("invoked", lowerScripts(input.Invoked)),
		field("mappings", lowerMappings(input.Mappings)), field("dependencies", lowerDependencies(input.Dependencies)),
		field("selected-scripts", selected), field("action-inputs", wacogo.NewValListOf(inputs...)),
	)
}

type liftError struct {
	what string
	got  wacogo.Val
}

func (e liftError) Error() string {
	return fmt.Sprintf("plugin returned %T where %s was expected", e.got, e.what)
}

func liftRecord(v wacogo.Val, what string) (*wacogo.ValRecord, error) {
	record, ok := v.(*wacogo.ValRecord)
	if !ok {
		return nil, liftError{what, v}
	}
	return record, nil
}

func liftString(record *wacogo.ValRecord, name string) (string, error) {
	s, ok := record.Field(name).(wacogo.ValString)
	if !ok {
		return "", liftError{"string field " + name, record.Field(name)}
	}
	return string(s), nil
}

func liftList(v wacogo.Val, what string) (*wacogo.ValList, error) {
	list, ok := v.(*wacogo.ValList)
	if !ok {
		return nil, liftError{what, v}
	}
	return list, nil
}

func liftPosition(v wacogo.Val) (sourcepos.Position, error) {
	record, err := liftRecord(v, "position")
	if err != nil {
		return sourcepos.Position{}, err
	}
	line, ok := record.Field("line").(wacogo.ValU32)
	if !ok {
		return sourcepos.Position{}, liftError{"position line", record.Field("line")}
	}
	column, ok := record.Field("column").(wacogo.ValU32)
	if !ok {
		return sourcepos.Position{}, liftError{"position column", record.Field("column")}
	}
	maxInt := uint64(^uint(0) >> 1)
	if line == 0 || column == 0 || uint64(line) > maxInt || uint64(column) > maxInt {
		return sourcepos.Position{}, fmt.Errorf("position coordinates must be one-based and fit the host integer range: line %d, column %d", line, column)
	}
	return sourcepos.Position{Line: int(line), Column: int(column)}, nil
}

func validateRange(start, end sourcepos.Position) error {
	if end.Line < start.Line || end.Line == start.Line && end.Column < start.Column {
		return fmt.Errorf("range end precedes start: %d:%d to %d:%d", start.Line, start.Column, end.Line, end.Column)
	}
	return nil
}

func liftDescriptor(v wacogo.Val) (checkinputs.Descriptor, error) {
	record, err := liftRecord(v, "descriptor")
	if err != nil {
		return checkinputs.Descriptor{}, err
	}
	name, err := liftString(record, "name")
	if err != nil {
		return checkinputs.Descriptor{}, err
	}
	requiredInputs, err := liftList(record.Field("required-inputs"), "descriptor required inputs")
	if err != nil {
		return checkinputs.Descriptor{}, err
	}
	descriptor := checkinputs.Descriptor{Descriptor: rulemeta.
		// The versioned analyzer ABI includes fix-availability. Ignore that legacy
		// claim: only the fixes on a diagnostic describe edits this host can offer.
		Descriptor{Name: name}}

	for key, dest := range map[string]*string{"description": &descriptor.Description, "category": &descriptor.Category, "subcategory": &descriptor.Subcategory, "maturity": &descriptor.Maturity, "provider": &descriptor.Provider, "explanation": &descriptor.Explanation, "help": &descriptor.Help, "documentation-url": &descriptor.DocumentationURL} {
		value, err := liftString(record, key)
		if err != nil {
			return checkinputs.Descriptor{}, err
		}
		*dest = value
	}
	recommended, ok := record.Field("recommended").(wacogo.ValBool)
	if !ok {
		return checkinputs.Descriptor{}, liftError{"recommended", record.Field("recommended")}
	}
	descriptor.Recommended = bool(recommended)
	schema, err := liftString(record, "options-schema")
	if err != nil {
		return checkinputs.Descriptor{}, err
	}
	descriptor.OptionsSchema, err = validOptionsSchema(schema)
	if err != nil {
		return checkinputs.Descriptor{}, err
	}

	examples, err := liftList(record.Field("examples"), "rule examples")
	if err != nil {
		return checkinputs.Descriptor{}, err
	}
	for i := range examples.Len() {
		record, err := liftRecord(examples.Get(i), "rule example")
		if err != nil {
			return checkinputs.Descriptor{}, err
		}
		example := rulemeta.Example{}
		for name, dest := range map[string]*string{"description": &example.Description, "before": &example.Before, "after": &example.After} {
			value, err := liftString(record, name)
			if err != nil {
				return checkinputs.Descriptor{}, err
			}
			*dest = value
		}
		descriptor.Examples = append(descriptor.Examples, example)
	}

	for i := range requiredInputs.Len() {
		inputKind, ok := requiredInputs.Get(i).(*wacogo.ValEnum)
		if !ok {
			return checkinputs.Descriptor{}, liftError{"input kind", requiredInputs.Get(i)}
		}
		if !validInputKindDiscriminant(inputKind.Discriminant()) {
			return checkinputs.Descriptor{}, fmt.Errorf("plugin requires unknown input kind %d", inputKind.Discriminant())
		}
		descriptor.RequiredInputs = append(descriptor.RequiredInputs, inputKinds[inputKind.Discriminant()])
	}
	return descriptor, nil
}

func liftEdits(v wacogo.Val) ([]results.Edit, error) {
	list, err := liftList(v, "edits")
	if err != nil {
		return nil, err
	}
	edits := make([]results.Edit, 0, list.Len())
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "edit")
		if err != nil {
			return nil, err
		}
		var edit results.Edit
		if edit.Path, err = liftString(record, "path"); err != nil {
			return nil, err
		}
		if edit.Start, err = liftPosition(record.Field("start")); err != nil {
			return nil, err
		}
		if edit.End, err = liftPosition(record.Field("end")); err != nil {
			return nil, err
		}
		if err := validateRange(edit.Start, edit.End); err != nil {
			return nil, err
		}
		if edit.Replacement, err = liftString(record, "replacement"); err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func liftFixes(v wacogo.Val) ([]results.Fix, error) {
	list, err := liftList(v, "fixes")
	if err != nil {
		return nil, err
	}
	if list.Len() == 0 {
		return nil, nil
	}
	fixes := make([]results.Fix, 0, list.Len())
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "fix")
		if err != nil {
			return nil, err
		}
		var fix results.Fix
		applicability, err := liftString(record, "applicability")
		if err != nil {
			return nil, err
		}
		if fix.Applicability, err = results.ParseApplicability(applicability); err != nil {
			return nil, err
		}
		if fix.Revisions, err = liftRevisions(record.Field("revisions")); err != nil {
			return nil, err
		}

		if fix.Description, err = liftString(record, "description"); err != nil {
			return nil, err
		}
		if fix.Edits, err = liftEdits(record.Field("edits")); err != nil {
			return nil, err
		}
		fixes = append(fixes, fix)
	}
	return fixes, nil
}

func liftDiagnostic(v wacogo.Val) (results.Diagnostic, error) {
	record, err := liftRecord(v, "diagnostic")
	if err != nil {
		return results.Diagnostic{}, err
	}
	var d results.Diagnostic
	reference, ok := record.Field("reference").(*wacogo.ValOption)
	if !ok {
		return d, liftError{"reference", record.Field("reference")}
	}
	if !reference.IsNone() {
		ref, err := liftReference(reference.Val())
		if err != nil {
			return d, err
		}
		d.Reference = &ref
	}
	locations, err := liftList(record.Field("related"), "related locations")
	if err != nil {
		return d, err
	}
	withheld := 0
	for i := range locations.Len() {
		location, err := liftRelated(wacogo.NewValListOf(locations.Get(i)))
		if err != nil {
			withheld++
			continue
		}
		d.Related = append(d.Related, location...)
	}

	for name, target := range map[string]*string{"rule": &d.Rule, "code": &d.Code, "severity": &d.Severity, "message": &d.Message, "path": &d.Path, "explanation": &d.Explanation, "help": &d.Help, "documentation-url": &d.DocumentationURL} {
		if *target, err = liftString(record, name); err != nil {
			return results.Diagnostic{}, err
		}
	}
	if d.Start, err = liftPosition(record.Field("start")); err != nil {
		return results.Diagnostic{}, err
	}
	if d.End, err = liftPosition(record.Field("end")); err != nil {
		return results.Diagnostic{}, err
	}
	if err := validateRange(d.Start, d.End); err != nil {
		return results.Diagnostic{}, err
	}
	if d.Fixes, err = liftFixes(record.Field("fixes")); err != nil {
		return results.Diagnostic{}, err
	}
	if withheld != 0 {
		return d, related.NewRejectedError(withheld)
	}
	return d, nil
}

func liftAnalysis(v wacogo.Val) ([]results.Diagnostic, error) {
	return liftAnalysisForInput(v, nil)
}

func liftAnalysisForInput(v wacogo.Val, input *checkinputs.Input) ([]results.Diagnostic, error) {
	result, err := liftRecord(v, "analysis outcome")
	if err != nil {
		return nil, err
	}
	completion, ok := result.Field("error").(*wacogo.ValOption)
	if !ok {
		return nil, liftError{"optional analysis error", result.Field("error")}
	}
	var analysisErr error
	if !completion.IsNone() {
		message, ok := completion.Val().(wacogo.ValString)
		if !ok {
			return nil, liftError{"analysis error message", completion.Val()}
		}
		analysisErr = fmt.Errorf("%s", string(message))
	}
	list, err := liftList(result.Field("diagnostics"), "diagnostics")
	if err != nil {
		return nil, err
	}
	diagnostics := make([]results.Diagnostic, 0, list.Len())
	withheld := 0
	for i := range list.Len() {
		d, diagnosticErr := liftDiagnostic(list.Get(i))
		if diagnosticErr != nil {
			if rejected, ok := errors.AsType[*related.RejectedError](diagnosticErr); ok {
				withheld += rejected.Count()
			} else {
				analysisErr = errors.Join(analysisErr, fmt.Errorf("diagnostic %d: %w", i, diagnosticErr))
				continue
			}
		}
		if input != nil {
			var bindingErr error
			d.Related, bindingErr = related.Bind(d.Related, input.Path, input.Revision, input.Coordinates)
			if rejected, ok := errors.AsType[*related.RejectedError](bindingErr); ok {
				withheld += rejected.Count()
			} else if bindingErr != nil {
				analysisErr = errors.Join(analysisErr, bindingErr)
			}
		}
		diagnostics = append(diagnostics, d)
	}
	if withheld != 0 {
		analysisErr = errors.Join(analysisErr, related.NewRejectedError(withheld))
	}
	return diagnostics, analysisErr
}

func lowerDependencies(deps []workflowfacts.Dependency) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(deps))
	for _, d := range deps {
		values = append(values, wacogo.NewValRecord(field("reference", lowerReference(d.Reference)), field("uses", wacogo.ValString(d.Uses)), field("kind", wacogo.ValString(d.Kind)), field("start", lowerPosition(d.Start)), field("end", lowerPosition(d.End))))
	}
	return wacogo.NewValListOf(values...)
}

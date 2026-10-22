package component

import (
	"errors"
	"fmt"
	"math"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/rule"
	"github.com/kjanat/wacogo"
)

// WIT enum order comes from the generated canonical contract, not a second table.
var factNames = func() []analyzer.Fact {
	facts := make([]analyzer.Fact, len(canonical_fact))
	for i, name := range canonical_fact {
		facts[i] = analyzer.Fact(name)
	}
	return facts
}()

var factDiscriminants = func() map[analyzer.Fact]uint32 {
	values := make(map[analyzer.Fact]uint32, len(factNames))
	for i, fact := range factNames {
		values[fact] = uint32(i)
	}
	return values
}()

var keyStateDiscriminants = func() map[analyzer.KeyState]uint32 {
	values := make(map[analyzer.KeyState]uint32, len(canonical_key_state))
	for i, name := range canonical_key_state {
		values[analyzer.KeyState(name)] = uint32(i)
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

func lowerPosition(p diagnostic.Position) *wacogo.ValRecord {
	return wacogo.NewValRecord(field("line", lowerU32(p.Line)), field("column", lowerU32(p.Column)))
}

func lowerFacts(facts []analyzer.Fact) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(facts))
	for _, fact := range facts {
		if discriminant, ok := factDiscriminants[fact]; ok {
			values = append(values, wacogo.NewValEnum(discriminant))
		}
	}
	return wacogo.NewValListOf(values...)
}

func lowerSpan(span analyzer.Span) *wacogo.ValRecord {
	documentEnd := wacogo.ValOptionNone()
	if span.DocumentEnd.Line != 0 {
		documentEnd = wacogo.ValOptionSome(lowerPosition(span.DocumentEnd))
	}
	return wacogo.NewValRecord(
		field("line", lowerU32(span.Line)), field("start", lowerU32(span.Start)), field("end", lowerU32(span.End)),
		field("document", lowerPosition(span.Document)), field("document-end", documentEnd),
	)
}

func lowerScript(script analyzer.Script) *wacogo.ValRecord {
	spans := make([]wacogo.Val, 0, len(script.Spans))
	for _, span := range script.Spans {
		spans = append(spans, lowerSpan(span))
	}
	invocation := wacogo.ValOptionNone()
	if script.Invocation != nil {
		invocation = wacogo.ValOptionSome(wacogo.NewValRecord(
			field("reference", lowerReference(script.Invocation.Reference)), field("caller", wacogo.ValString(script.Invocation.Caller)), field("step", lowerPosition(script.Invocation.Step)),
			field("uses", wacogo.ValString(script.Invocation.Uses)), field("depth", lowerU32(script.Invocation.Depth)),
		))
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
		field("spans", wacogo.NewValListOf(spans...)), field("invocation", invocation), field("runtime", runtime),
	)
}

func lowerScripts(scripts []analyzer.Script) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(scripts))
	for _, script := range scripts {
		values = append(values, lowerScript(script))
	}
	return wacogo.NewValListOf(values...)
}

func lowerMappings(mappings []analyzer.Mapping) *wacogo.ValList {
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

func lowerInput(input analyzer.Input) *wacogo.ValRecord {
	return wacogo.NewValRecord(
		field("reference", lowerReference(input.Reference)), field("path", wacogo.ValString(input.Path)), field("content", wacogo.ValString(input.Content)),
		field("kind", wacogo.ValString(input.Kind)), field("parse-status", wacogo.ValString(input.ParseStatus)),
		field("available", lowerFacts(input.Available)), field("options", wacogo.ValString(string(input.Options))),
		field("scripts", lowerScripts(input.Scripts)), field("invoked", lowerScripts(input.Invoked)),
		field("mappings", lowerMappings(input.Mappings)), field("dependencies", lowerDependencies(input.Dependencies)),
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

func liftPosition(v wacogo.Val) (diagnostic.Position, error) {
	record, err := liftRecord(v, "position")
	if err != nil {
		return diagnostic.Position{}, err
	}
	line, ok := record.Field("line").(wacogo.ValU32)
	if !ok {
		return diagnostic.Position{}, liftError{"position line", record.Field("line")}
	}
	column, ok := record.Field("column").(wacogo.ValU32)
	if !ok {
		return diagnostic.Position{}, liftError{"position column", record.Field("column")}
	}
	maxInt := uint64(^uint(0) >> 1)
	if line == 0 || column == 0 || uint64(line) > maxInt || uint64(column) > maxInt {
		return diagnostic.Position{}, fmt.Errorf("position coordinates must be one-based and fit the host integer range: line %d, column %d", line, column)
	}
	return diagnostic.Position{Line: int(line), Column: int(column)}, nil
}

func validateRange(start, end diagnostic.Position) error {
	if end.Line < start.Line || end.Line == start.Line && end.Column < start.Column {
		return fmt.Errorf("range end precedes start: %d:%d to %d:%d", start.Line, start.Column, end.Line, end.Column)
	}
	return nil
}

func liftDescriptor(v wacogo.Val) (analyzer.Descriptor, error) {
	record, err := liftRecord(v, "descriptor")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	name, err := liftString(record, "name")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	requires, err := liftList(record.Field("requires"), "descriptor requires")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	descriptor := analyzer.Descriptor{Name: name}
	for key, dest := range map[string]*string{"description": &descriptor.Description, "category": &descriptor.Category, "subcategory": &descriptor.Subcategory, "maturity": &descriptor.Maturity, "provider": &descriptor.Provider, "explanation": &descriptor.Explanation, "help": &descriptor.Help, "documentation-url": &descriptor.DocumentationURL, "fix-availability": &descriptor.FixAvailability} {
		value, err := liftString(record, key)
		if err != nil {
			return analyzer.Descriptor{}, err
		}
		*dest = value
	}
	recommended, ok := record.Field("recommended").(wacogo.ValBool)
	if !ok {
		return analyzer.Descriptor{}, liftError{"recommended", record.Field("recommended")}
	}
	descriptor.Recommended = bool(recommended)
	schema, err := liftString(record, "options-schema")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	descriptor.OptionsSchema, err = validOptionsSchema(schema)
	if err != nil {
		return analyzer.Descriptor{}, err
	}

	examples, err := liftList(record.Field("examples"), "rule examples")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	for i := range examples.Len() {
		record, err := liftRecord(examples.Get(i), "rule example")
		if err != nil {
			return analyzer.Descriptor{}, err
		}
		example := rule.Example{}
		for name, dest := range map[string]*string{"description": &example.Description, "before": &example.Before, "after": &example.After} {
			value, err := liftString(record, name)
			if err != nil {
				return analyzer.Descriptor{}, err
			}
			*dest = value
		}
		descriptor.Examples = append(descriptor.Examples, example)
	}

	for i := range requires.Len() {
		fact, ok := requires.Get(i).(*wacogo.ValEnum)
		if !ok {
			return analyzer.Descriptor{}, liftError{"fact", requires.Get(i)}
		}
		if int(fact.Discriminant()) >= len(factNames) {
			return analyzer.Descriptor{}, fmt.Errorf("plugin requires unknown fact %d", fact.Discriminant())
		}
		descriptor.Requires = append(descriptor.Requires, factNames[fact.Discriminant()])
	}
	return descriptor, nil
}

func liftEdits(v wacogo.Val) ([]diagnostic.Edit, error) {
	list, err := liftList(v, "edits")
	if err != nil {
		return nil, err
	}
	edits := make([]diagnostic.Edit, 0, list.Len())
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "edit")
		if err != nil {
			return nil, err
		}
		var edit diagnostic.Edit
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

func liftFixes(v wacogo.Val) ([]diagnostic.Fix, error) {
	list, err := liftList(v, "fixes")
	if err != nil {
		return nil, err
	}
	if list.Len() == 0 {
		return nil, nil
	}
	fixes := make([]diagnostic.Fix, 0, list.Len())
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "fix")
		if err != nil {
			return nil, err
		}
		var fix diagnostic.Fix
		if fix.Applicability, err = liftString(record, "applicability"); err != nil {
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

func liftDiagnostic(v wacogo.Val) (diagnostic.Diagnostic, error) {
	record, err := liftRecord(v, "diagnostic")
	if err != nil {
		return diagnostic.Diagnostic{}, err
	}
	var d diagnostic.Diagnostic
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
	if d.Related, err = liftRelated(record.Field("related")); err != nil {
		return d, err
	}

	for name, target := range map[string]*string{"rule": &d.Rule, "code": &d.Code, "severity": &d.Severity, "message": &d.Message, "path": &d.Path, "explanation": &d.Explanation, "help": &d.Help, "documentation-url": &d.DocumentationURL} {
		if *target, err = liftString(record, name); err != nil {
			return diagnostic.Diagnostic{}, err
		}
	}
	if d.Start, err = liftPosition(record.Field("start")); err != nil {
		return diagnostic.Diagnostic{}, err
	}
	if d.End, err = liftPosition(record.Field("end")); err != nil {
		return diagnostic.Diagnostic{}, err
	}
	if err := validateRange(d.Start, d.End); err != nil {
		return diagnostic.Diagnostic{}, err
	}
	if d.Fixes, err = liftFixes(record.Field("fixes")); err != nil {
		return diagnostic.Diagnostic{}, err
	}
	return d, nil
}

func liftAnalysis(v wacogo.Val) ([]diagnostic.Diagnostic, error) {
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
	diagnostics := make([]diagnostic.Diagnostic, 0, list.Len())
	for i := range list.Len() {
		d, diagnosticErr := liftDiagnostic(list.Get(i))
		if diagnosticErr != nil {
			analysisErr = errors.Join(analysisErr, fmt.Errorf("diagnostic %d: %w", i, diagnosticErr))
			continue
		}
		diagnostics = append(diagnostics, d)
	}
	return diagnostics, analysisErr
}

func lowerDependencies(deps []analyzer.Dependency) *wacogo.ValList {
	values := make([]wacogo.Val, 0, len(deps))
	for _, d := range deps {
		values = append(values, wacogo.NewValRecord(field("reference", lowerReference(d.Reference)), field("uses", wacogo.ValString(d.Uses)), field("kind", wacogo.ValString(d.Kind)), field("start", lowerPosition(d.Start)), field("end", lowerPosition(d.End))))
	}
	return wacogo.NewValListOf(values...)
}

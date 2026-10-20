package component

import (
	"fmt"
	"math"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"github.com/partite-ai/wacogo"
)

var factDiscriminants = map[analyzer.Fact]uint32{
	analyzer.Source: 0, analyzer.Scripts: 1, analyzer.Mappings: 2, analyzer.Invocations: 3, analyzer.Execution: 4,
}

var factNames = []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Mappings, analyzer.Invocations, analyzer.Execution}

var keyStateDiscriminants = map[analyzer.KeyState]uint32{analyzer.UnknownKey: 0, analyzer.KnownKey: 1, analyzer.InvalidKey: 2}

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
			field("caller", wacogo.ValString(script.Invocation.Caller)), field("step", lowerPosition(script.Invocation.Step)),
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
		field("job-id", wacogo.ValString(script.JobID)), field("step-id", wacogo.ValString(script.StepID)),
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
			field("scope", wacogo.ValString(mapping.Scope)), field("complete", wacogo.ValBool(mapping.Complete)),
			field("entries", wacogo.NewValListOf(entries...)),
		))
	}
	return wacogo.NewValListOf(values...)
}

func lowerInput(input analyzer.Input) *wacogo.ValRecord {
	return wacogo.NewValRecord(
		field("path", wacogo.ValString(input.Path)), field("content", wacogo.ValString(input.Content)),
		field("kind", wacogo.ValString(input.Kind)), field("parse-status", wacogo.ValString(input.ParseStatus)),
		field("available", lowerFacts(input.Available)), field("options", wacogo.ValString(string(input.Options))),
		field("scripts", lowerScripts(input.Scripts)), field("invoked", lowerScripts(input.Invoked)),
		field("mappings", lowerMappings(input.Mappings)),
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
	return diagnostic.Position{Line: int(line), Column: int(column)}, nil
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
	for name, target := range map[string]*string{"rule": &d.Rule, "code": &d.Code, "severity": &d.Severity, "message": &d.Message, "path": &d.Path} {
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
	if d.Fixes, err = liftFixes(record.Field("fixes")); err != nil {
		return diagnostic.Diagnostic{}, err
	}
	return d, nil
}

func liftAnalysis(v wacogo.Val) ([]diagnostic.Diagnostic, error) {
	result, ok := v.(*wacogo.ValResult)
	if !ok {
		return nil, liftError{"analysis result", v}
	}
	if !result.IsOk() {
		message, ok := result.Err().(wacogo.ValString)
		if !ok {
			return nil, liftError{"error message", result.Err()}
		}
		return nil, fmt.Errorf("%s", string(message))
	}
	list, err := liftList(result.Ok(), "diagnostics")
	if err != nil {
		return nil, err
	}
	diagnostics := make([]diagnostic.Diagnostic, 0, list.Len())
	for i := range list.Len() {
		d, err := liftDiagnostic(list.Get(i))
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, d)
	}
	return diagnostics, nil
}

package parser

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	stringutil "actionlint.kjanat.dev/v2/internal/stringutil"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/schema"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// NodeKindName describes the kind of a YAML node.
func NodeKindName(k syntax.Kind) string {
	switch k {
	case syntax.DocumentNode:
		return "document"
	case syntax.SequenceNode:
		return "sequence"
	case syntax.MappingNode:
		return "mapping"
	case syntax.ScalarNode:
		return "scalar"
	case syntax.AliasNode:
		// Rejected recursive or undeclared uses remain aliases in the projection.
		return "alias"
	default:
		panic(fmt.Sprintf("unreachable: unknown YAML kind: %v", k))
	}
}

func PosAt(n *syntax.Node) *workflowlang.Pos {
	return &workflowlang.Pos{Line: n.Line, Col: n.Column}
}

func newString(n *syntax.Node) *workflowlang.String {
	quoted := n.Style&(syntax.DoubleQuotedStyle|syntax.SingleQuotedStyle) != 0
	var kind workflowlang.SourceScalarKind
	switch n.Kind {
	case syntax.DocumentNode:
		kind = workflowlang.SourceScalarDocument
	case syntax.SequenceNode:
		kind = workflowlang.SourceScalarSequence
	case syntax.MappingNode:
		kind = workflowlang.SourceScalarMapping
	case syntax.ScalarNode:
		kind = workflowlang.SourceScalarScalar
	case syntax.AliasNode:
		kind = workflowlang.SourceScalarAlias
	default:
		return &workflowlang.String{Value: n.Value, Quoted: quoted, Pos: PosAt(n)}
	}
	pos := workflowlang.Pos{Line: n.Line, Col: n.Column}
	return &workflowlang.String{
		Value:  n.Value,
		Quoted: quoted,
		Pos:    &workflowlang.Pos{Line: pos.Line, Col: pos.Col},
		SourceScalar: workflowlang.SourceScalar{
			HasSource: true,
			Kind:      kind,
			Tag:       n.Tag,
			Value:     n.Value,
			Pos:       pos,
		},
	}
}

// workflowMappingEntry represents a key-value entry in YAML mapping.
type workflowMappingEntry struct {
	// id is a key in lower case for comparing case-insensitive keys.
	id  string
	key *workflowlang.String
	val *syntax.Node
}

type delayedSprintf struct {
	result string
	// Note: Currently only one String arg is sufficient and it's faster than keeping generic any args.
	// `arg` must not be empty when it is used. Empty value means the argument is unused.
	arg string
}

func sprintf(fmt, arg string) delayedSprintf {
	return delayedSprintf{fmt, arg}
}

func (l *delayedSprintf) String() string {
	if len(l.arg) > 0 {
		// This delayed formatting reduces the number of allocations on parsing workflow by 4.94%
		l.result = fmt.Sprintf(l.result, l.arg)
		l.arg = ""
	}
	return l.result
}

type Parser struct {
	scalarDocument     *sourcemap.Document
	errs               []*SyntaxError
	sourceLines        []string
	aliases            map[*syntax.Node]*syntax.Node
	scalarContexts     map[workflowlang.Pos]sourcemap.Context
	scalarSources      map[workflowlang.Pos]*workflowlang.ScriptSource
	scalarComments     map[workflowlang.Pos]scalarCommentNodes
	scalarDeclarations map[workflowlang.Pos]scalarCommentNodes
}

func NewSourceParser(source []byte) *Parser {
	document := sourcemap.NewDocument(string(source))
	return &Parser{sourceLines: document.Lines(), scalarDocument: document}
}

func (p *Parser) error(n *syntax.Node, code Code, m string) {
	p.errs = append(p.errs, &SyntaxError{Message: m, Line: n.Line, Column: n.Column, Kind: "syntax-check", Code: code})
}

func (p *Parser) errorAt(pos *workflowlang.Pos, code Code, m string) {
	p.errs = append(p.errs, &SyntaxError{Message: m, Line: pos.Line, Column: pos.Col, Kind: "syntax-check", Code: code})
}

func (p *Parser) errorfAt(pos *workflowlang.Pos, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.errorAt(pos, code, m)
}

func (p *Parser) errorf(n *syntax.Node, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.error(n, code, m)
}

func (p *Parser) typeErrorf(n *syntax.Node, format string, args ...any) {
	p.kindErrorf(n, CodeTypeMismatch, format, args...)
}

// kindErrorf reports a node of the wrong kind and points an aliased node back at its anchor.
func (p *Parser) kindErrorf(n *syntax.Node, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	if alias, ok := p.aliases[n]; ok {
		m += fmt.Sprintf(". alias %q refers to the anchor at line:%d, column:%d", alias.Value, n.Line, n.Column)
		n = alias
	}
	p.error(n, code, m)
}

func (p *Parser) unexpectedKey(s *workflowlang.String, sec string, expected []string) {
	if !strings.ContainsRune(sec, ' ') {
		sec = fmt.Sprintf("%q section", sec)
	}
	var m string
	switch l := len(expected); {
	case l == 1:
		m = fmt.Sprintf("expected %q key for %s but got %q", expected[0], sec, s.Value)
	case l > 1:
		m = fmt.Sprintf("unexpected key %q for %s. expected one of %v", s.Value, sec, stringutil.SortedQuotes(expected))
	default:
		m = fmt.Sprintf("unexpected key %q for %s", s.Value, sec)
	}
	p.errorAt(s.Pos, CodeUnknownKey, m)
}

func (p *Parser) checkNotEmpty(sec string, count int, n *syntax.Node) bool {
	if count == 0 {
		p.errorf(n, CodeEmptyValue, "%q section should not be empty", sec)
		return false
	}
	return true
}

func (p *Parser) checkSequence(sec string, n *syntax.Node, allowEmpty bool) bool {
	if n.Kind != syntax.SequenceNode {
		p.typeErrorf(n, "%q section must be sequence node but got %s node with %q tag", sec, NodeKindName(n.Kind), n.Tag)
		return false
	}
	return allowEmpty || p.checkNotEmpty(sec, len(n.Content), n)
}

func (p *Parser) CheckString(n *syntax.Node, allowEmpty bool) bool {
	// Do not check n.Tag is !!str because we don't need to check the node is String strictly.
	// In almost all cases, other nodes (like 42) are handled as String with its String representation.
	if n.Kind != syntax.ScalarNode {
		p.typeErrorf(n, "expected scalar node for string value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return false
	}
	if !p.checkRawYAMLTag(n) {
		return false
	}
	if !allowEmpty && n.Value == "" {
		p.error(n, CodeEmptyValue, "string should not be empty")
		return false
	}
	return true
}

func (p *Parser) missingExpression(n *syntax.Node, expecting string) {
	p.kindErrorf(n, CodeExpressionForm, "expecting a single ${{...}} expression or %s, but found plain text node", expecting)
}

func (p *Parser) parseExpression(n *syntax.Node, expecting string) *workflowlang.String {
	if !workflowlang.IsExprAssigned(n.Value) {
		p.missingExpression(n, expecting)
		return nil
	}
	return newString(n)
}

func (p *Parser) mayParseExpression(n *syntax.Node) *workflowlang.String {
	if n.Tag != workflowlang.YamlTagStr {
		return nil
	}
	if !workflowlang.IsExprAssigned(n.Value) {
		return nil
	}
	return newString(n)
}

func (p *Parser) parseString(n *syntax.Node, allowEmpty bool) *workflowlang.String {
	if !p.CheckString(n, allowEmpty) {
		return &workflowlang.String{Value: "", Quoted: false, Pos: PosAt(n)}
	}
	return newString(n)
}

// parseCondition preserves the distinction between YAML null and the String "null".
// GitHub normalises plain YAML booleans such as True or FALSE before compiling the
// condition, so they read as the expression literals true and false.
func (p *Parser) parseCondition(n *syntax.Node) *workflowlang.String {
	s := p.parseString(n, true)
	switch n.Tag {
	case workflowlang.YamlTagNull:
		s.Value = ""
	case workflowlang.YamlTagBool:
		s.Value = strings.ToLower(s.Value)
	}
	if strings.TrimSpace(s.Value) == "" {
		s.Value = "success()"
	}
	return s
}

// parseStaticString folds a sole literal expression before downstream validation.
func (p *Parser) parseStaticString(n *syntax.Node) *workflowlang.String {
	if value := exprlang.LiteralExpressionValue(n.Value); value != nil {
		source := newString(n).SourceScalar
		literal := *n
		literal.Value = *value
		result := p.parseString(&literal, false)
		if result.SourceScalar.HasSource {
			source.Evaluated = true
			result.SourceScalar = source
		}
		return result
	}
	return p.parseString(n, false)
}

func (p *Parser) parseStringSequence(sec string, n *syntax.Node, allowEmpty bool) []*workflowlang.String {
	if ok := p.checkSequence(sec, n, allowEmpty); !ok {
		return nil
	}

	ss := make([]*workflowlang.String, 0, len(n.Content))
	for _, c := range n.Content {
		s := p.parseString(c, false)
		if s != nil {
			ss = append(ss, s)
		}
	}
	return ss
}

func (p *Parser) parseStringOrStringSequence(sec string, n *syntax.Node) []*workflowlang.String {
	if n.Kind == syntax.ScalarNode {
		return []*workflowlang.String{p.parseString(n, false)}
	}
	return p.parseStringSequence(sec, n, false)
}

func (p *Parser) parseLiteralBool(n *syntax.Node) *workflowlang.Bool {
	if n.Kind != syntax.ScalarNode || n.Tag != workflowlang.YamlTagBool {
		p.typeErrorf(n, "expected boolean literal but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	return p.parseBool(n)
}

func (p *Parser) parseBool(n *syntax.Node) *workflowlang.Bool {
	if n.Kind != syntax.ScalarNode || (n.Tag != workflowlang.YamlTagBool && n.Tag != workflowlang.YamlTagStr) {
		p.typeErrorf(n, "expected bool value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == workflowlang.YamlTagStr {
		e := p.parseExpression(n, "boolean literal \"true\" or \"false\"")
		return &workflowlang.Bool{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	return &workflowlang.Bool{
		Value: strings.EqualFold(n.Value, "true"),
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseInt(n *syntax.Node) *workflowlang.Int {
	if n.Kind != syntax.ScalarNode || (n.Tag != workflowlang.YamlTagInt && n.Tag != workflowlang.YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for integer value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == workflowlang.YamlTagStr {
		e := p.parseExpression(n, "integer literal")
		if e == nil {
			return nil
		}
		return &workflowlang.Int{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	i, err := parseYAMLInteger(n.Value)
	if err != nil {
		p.errorf(n, CodeInvalidValue, "invalid integer value: %q: %s", n.Value, err.Error())
		return nil
	}

	return &workflowlang.Int{
		Value: i,
		Pos:   PosAt(n),
	}
}

// parseYAMLInteger follows the runner's decimal, hexadecimal and octal grammar.
// Radix-prefixed integers use the runner's signed 32-bit representation.
func parseYAMLInteger(value string) (int, error) {
	base := 0
	if strings.HasPrefix(value, "0x") {
		base = 16
	} else if strings.HasPrefix(value, "0o") {
		base = 8
	}
	if base == 0 {
		return strconv.Atoi(value)
	}
	if !schema.IsCoreSchemaInt(value) {
		return 0, errors.New("invalid YAML integer")
	}
	n, err := strconv.ParseUint(value[2:], base, 32)
	if err != nil {
		return 0, err
	}
	// Decode the runner's two's-complement representation without narrowing
	// an unsigned value into a signed type. Both branches fit a 32-bit int.
	if n <= math.MaxInt32 {
		return int(n), nil
	}
	return -1 - int(math.MaxUint32-n), nil
}

func (p *Parser) parseFloat(n *syntax.Node) *workflowlang.Float {
	if n.Kind != syntax.ScalarNode || (n.Tag != workflowlang.YamlTagFloat && n.Tag != workflowlang.YamlTagInt && n.Tag != workflowlang.YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for float value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == workflowlang.YamlTagStr {
		e := p.parseExpression(n, "float number literal")
		if e == nil {
			return nil
		}
		return &workflowlang.Float{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	var f float64
	var err error
	if strings.HasPrefix(n.Value, "0x") || strings.HasPrefix(n.Value, "0o") {
		var i int
		i, err = parseYAMLInteger(n.Value)
		f = float64(i)
	} else {
		f, err = strconv.ParseFloat(n.Value, 64)
	}
	if err != nil {
		p.errorf(n, CodeInvalidValue, "invalid float value: %q: %s", n.Value, err.Error())
		return nil
	}
	if math.IsNaN(f) {
		p.errorf(n, CodeInvalidValue, "invalid float value: %q: NaN is not a finite number", n.Value)
		return nil
	}

	return &workflowlang.Float{
		Value: f,
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseMapping(where delayedSprintf, n *syntax.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMappingEntries(where, n, allowEmpty, caseSensitive, false)
}

func (p *Parser) parseMappingEntries(where delayedSprintf, n *syntax.Node, allowEmpty, caseSensitive, insertions bool) iter.Seq[workflowMappingEntry] {
	return func(yield func(workflowMappingEntry) bool) {
		if n.Kind == syntax.ScalarNode && n.Tag == workflowlang.YamlTagNull {
			if !allowEmpty {
				p.errorf(n, CodeEmptyValue, "%s should not be empty. please remove this section if it's unnecessary", where.String())
			}
			return
		}

		if n.Kind != syntax.MappingNode {
			p.typeErrorf(n, "%s is %s node but mapping node is expected", where.String(), NodeKindName(n.Kind))
			return
		}

		keys := make(map[string]*workflowlang.Pos, len(n.Content)/2)
		empty := true
		for i := 0; i < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			if insertions && keyNode.Kind == syntax.ScalarNode {
				if resolved, known := knownMappingKey(newString(keyNode)); known {
					resolvedNode := *keyNode
					resolvedNode.Value = resolved.Value
					keyNode = &resolvedNode
				}
			}
			if !p.CheckString(keyNode, false) {
				p.checkRawYAMLTags(n.Content[i+1])
				empty = false
				continue
			}
			k := newString(n.Content[i])
			if insertions && emptyResolvedMappingKey(workflowlang.SemanticValue(k)) {
				p.errorAt(k.Pos, CodeEmptyValue, "string should not be empty")
				p.checkRawYAMLTags(n.Content[i+1])
				empty = false
				continue
			}

			if k.Value == "<<" {
				p.errorAt(k.Pos, CodeMergeKey, "GitHub Actions does not support YAML merge key \"<<\"")
				continue
			}

			id := keyNode.Value
			if !caseSensitive {
				// Keys of mappings are sometimes case insensitive. For example, following matrix is invalid.
				//   matrix:
				//     foo: [1, 2, 3]
				//     FOO: [1, 2, 3]
				// To detect case insensitive duplicate keys, we use lowercase keys
				id = strings.ToLower(id)
			}

			if insertions && exprlang.IsMappingInsertion(k.Value) {
				if !yield(workflowMappingEntry{id, k, n.Content[i+1]}) {
					break
				}
				inserted := resolveInsertedMapping(n.Content[i+1])
				insertedKeys := map[string]*workflowlang.String{}
				for declaration := range inserted.Declarations() {
					switch declaration.State {
					case mapping.KnownKey:
						origin := declaration.Origin.Span.Start
						insertedKeys[strings.ToLower(declaration.Name)] = &workflowlang.String{
							Value: declaration.Name,
							Pos:   &workflowlang.Pos{Line: origin.Line, Col: origin.Column},
						}
					case mapping.InvalidKey:
						if n.Content[i+1].Kind == syntax.ScalarNode && emptyResolvedMappingKey(declaration.Key) {
							origin := declaration.Origin.Span.Start
							pos := &workflowlang.Pos{Line: origin.Line, Col: origin.Column}
							if origin.Line == 0 || origin.Column == 0 {
								pos = PosAt(n.Content[i+1])
							}
							p.errorAt(pos, CodeEmptyValue, "string should not be empty")
						}
					case mapping.UnknownKey:
					}
				}
				for _, name := range slices.Sorted(maps.Keys(insertedKeys)) {
					inserted := insertedKeys[name]
					if previous, duplicate := keys[name]; duplicate {
						p.errorfAt(inserted.Pos, CodeDuplicateKey, "key %q is duplicated in %s. previously defined at %s. note that this key is case insensitive", inserted.Value, where.String(), previous.String())
					} else {
						keys[name] = inserted.Pos
					}
				}
				empty = false
				continue
			}
			if pos, ok := keys[id]; ok {
				var note string
				if !caseSensitive {
					note = ". note that this key is case insensitive"
				}
				p.errorfAt(k.Pos, CodeDuplicateKey, "key %q is duplicated in %s. previously defined at %s%s", k.Value, where.String(), pos.String(), note)
				continue
			}

			if !yield(workflowMappingEntry{id, k, n.Content[i+1]}) {
				break
			}

			keys[id] = k.Pos
			empty = false
		}

		if !allowEmpty && empty {
			p.errorf(n, CodeEmptyValue, "%s should not be empty. please remove this section if it's unnecessary", where.String())
		}
	}
}

func (p *Parser) parseSectionMapping(section string, n *syntax.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf("%q section", section), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseMappingAt(where string, n *syntax.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf(where, ""), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseScheduleEvent(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.ScheduledEvent {
	if ok := p.checkSequence("schedule", n, false); !ok {
		return nil
	}

	schedules := make([]*workflowlang.ScheduleEntry, 0, len(n.Content))
	for _, c := range n.Content {
		entry := &workflowlang.ScheduleEntry{}
		cronGiven := false
		for e := range p.parseMappingAt("element of \"schedule\" section", c, false, true) {
			switch e.id {
			case "cron":
				cronGiven = true
				if s := p.parseString(e.val, false); s.Value != "" {
					entry.Cron = s
				}
			case "timezone":
				if s := p.parseString(e.val, false); s.Value != "" {
					entry.Timezone = s
				}
			default:
				p.unexpectedKey(e.key, "element of \"schedule\" section", []string{"cron", "timezone"})
			}
		}
		if entry.Cron != nil {
			schedules = append(schedules, entry)
		} else if !cronGiven && c.Kind == syntax.MappingNode && len(c.Content) != 0 {
			p.error(c, CodeMissingKey, "\"cron\" is missing in element of \"schedule\" section")
		}
	}

	return &workflowlang.ScheduledEvent{Schedules: schedules, Pos: pos}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatchinputs
func (p *Parser) parseWorkflowDispatchEventInput(name *workflowlang.String, n *syntax.Node) *workflowlang.DispatchInput {
	ret := &workflowlang.DispatchInput{Name: name}

	for e := range p.parseMappingAt("input settings of workflow_dispatch event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseLiteralBool(e.val)
		case "default":
			ret.Default = p.parseString(e.val, true)
		case "type":
			if !p.CheckString(e.val, false) {
				continue
			}
			switch e.val.Value {
			case "string":
				ret.Type = workflowlang.WorkflowDispatchEventInputTypeString
			case "number":
				ret.Type = workflowlang.WorkflowDispatchEventInputTypeNumber
			case "boolean":
				ret.Type = workflowlang.WorkflowDispatchEventInputTypeBoolean
			case "choice":
				ret.Type = workflowlang.WorkflowDispatchEventInputTypeChoice
			case "environment":
				ret.Type = workflowlang.WorkflowDispatchEventInputTypeEnvironment
			default:
				p.errorf(e.val, CodeInvalidValue, `input type of workflow_dispatch event must be one of "string", "number", "boolean", "choice", "environment" but got %q`, e.val.Value)
			}
		case "options":
			if p.checkSequence("options", e.val, false) {
				for _, option := range e.val.Content {
					ret.Options = append(ret.Options, p.parseString(option, true))
				}
			}
		default:
			p.unexpectedKey(e.key, "inputs", []string{"description", "required", "default", "type", "options"})
		}
	}

	return ret
}

// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
// - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatch
func (p *Parser) parseWorkflowDispatchEvent(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.WorkflowDispatchEvent {
	ret := &workflowlang.WorkflowDispatchEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_dispatch", n, true, true) {
		if e.id != "inputs" {
			p.unexpectedKey(e.key, "workflow_dispatch", []string{"inputs"})
			continue
		}

		ret.Inputs = map[string]*workflowlang.DispatchInput{}
		for e := range p.parseSectionMapping("inputs", e.val, true, false) {
			ret.Inputs[e.id] = p.parseWorkflowDispatchEventInput(e.key, e.val)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#repository_dispatch
func (p *Parser) parseRepositoryDispatchEvent(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.RepositoryDispatchEvent {
	ret := &workflowlang.RepositoryDispatchEvent{Pos: pos}

	// Note: Omitting 'types' is ok. In the case, all types trigger the workflow
	for e := range p.parseSectionMapping("repository_dispatch", n, true, true) {
		if e.id == "types" {
			ret.Types = p.parseStringOrStringSequence("types", e.val)
		} else {
			p.unexpectedKey(e.key, "repository_dispatch", []string{"types"})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#using-filters
func (p *Parser) parseWebhookEventFilter(name *workflowlang.String, n *syntax.Node) *workflowlang.WebhookEventFilter {
	v := p.parseStringOrStringSequence(name.Value, n)
	return &workflowlang.WebhookEventFilter{Name: name, Values: v}
}

func (p *Parser) parseWebhookEvent(name *workflowlang.String, n *syntax.Node) *workflowlang.WebhookEvent {
	ret := &workflowlang.WebhookEvent{Hook: name, Pos: name.Pos}

	// Note: 'tags', 'tags-ignore', 'branches', 'branches-ignore' can be empty. Since there are
	// some cases where setting empty values to them is necessary.
	//
	// > If only define only tags filter (tags/tags-ignore) or only branches filter
	// > (branches/branches-ignore) for on.push, the workflow won’t run for events affecting the
	// > undefined Git ref.
	//
	// https://github.community/t/using-on-push-tags-ignore-and-paths-ignore-together/16931
	for e := range p.parseSectionMapping(name.Value, n, true, true) {
		// Note: Glob pattern cannot be empty, but it is checked by 'glob' rule with better error
		// message. So Parser allows empty patterns here.
		switch e.id {
		case "types":
			ret.Types = p.parseStringOrStringSequence(e.key.Value, e.val)
		case "branches":
			ret.Branches = p.parseWebhookEventFilter(e.key, e.val)
		case "branches-ignore":
			ret.BranchesIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "tags":
			ret.Tags = p.parseWebhookEventFilter(e.key, e.val)
		case "tags-ignore":
			ret.TagsIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "paths":
			ret.Paths = p.parseWebhookEventFilter(e.key, e.val)
		case "paths-ignore":
			ret.PathsIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "workflows":
			ret.Workflows = p.parseStringOrStringSequence(e.key.Value, e.val)
		default:
			p.unexpectedKey(e.key, name.Value, []string{
				"types",
				"branches",
				"branches-ignore",
				"tags",
				"tags-ignore",
				"paths",
				"paths-ignore",
				"workflows",
			})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onworkflow_callinputs
func (p *Parser) parseWorkflowCallEventInput(id string, name *workflowlang.String, n *syntax.Node) *workflowlang.WorkflowCallEventInput {
	ret := &workflowlang.WorkflowCallEventInput{Name: name, ID: id}
	typed := false

	for e := range p.parseMappingAt("input of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseLiteralBool(e.val)
		case "default":
			ret.Default = p.parseString(e.val, true)
		case "type":
			typed = true
			if !p.CheckString(e.val, false) {
				continue
			}
			switch e.val.Value {
			case "boolean":
				ret.Type = workflowlang.WorkflowCallEventInputTypeBoolean
			case "number":
				ret.Type = workflowlang.WorkflowCallEventInputTypeNumber
			case "string":
				ret.Type = workflowlang.WorkflowCallEventInputTypeString
			default:
				p.errorf(e.val, CodeInvalidValue, "invalid value %q for input type of workflow_call event. it must be one of \"boolean\", \"number\", or \"string\"", e.val.Value)
			}
		default:
			p.unexpectedKey(e.key, "inputs at workflow_call event", []string{"description", "required", "default", "type"})
		}
	}

	if !typed {
		p.errorfAt(name.Pos, CodeMissingKey, "\"type\" is missing at %q input of workflow_call event", name.Value)
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#example-of-onworkflow_callsecrets
func (p *Parser) parseWorkflowCallEventSecret(name *workflowlang.String, n *syntax.Node) *workflowlang.WorkflowCallEventSecret {
	ret := &workflowlang.WorkflowCallEventSecret{Name: name}

	for e := range p.parseMappingAt("secret of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseLiteralBool(e.val)
		default:
			p.unexpectedKey(e.key, "secrets", []string{"description", "required"})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#example-of-onworkflow_calloutputs
func (p *Parser) parseWorkflowCallEventOutput(name *workflowlang.String, n *syntax.Node) *workflowlang.WorkflowCallEventOutput {
	output := &workflowlang.WorkflowCallEventOutput{Name: name}

	for e := range p.parseMappingAt("output of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			output.Description = p.parseString(e.val, true)
		case "value":
			output.Value = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "outputs at workflow_call event", []string{"description", "value"})
		}
	}

	if output.Value == nil {
		p.errorfAt(name.Pos, CodeMissingKey, "\"value\" is missing at %q output of workflow_call event", name.Value)
	}

	return output
}

// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow-reuse-events
// - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_call
// - https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
func (p *Parser) parseWorkflowCallEvent(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.WorkflowCallEvent {
	ret := &workflowlang.WorkflowCallEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_call", n, true, true) {
		switch e.id {
		case "inputs":
			ret.Inputs = []*workflowlang.WorkflowCallEventInput{}
			for e := range p.parseSectionMapping("inputs", e.val, true, false) {
				ret.Inputs = append(ret.Inputs, p.parseWorkflowCallEventInput(e.id, e.key, e.val))
			}
		case "secrets":
			ret.Secrets = map[string]*workflowlang.WorkflowCallEventSecret{}
			for e := range p.parseSectionMapping("secrets", e.val, true, false) {
				ret.Secrets[e.id] = p.parseWorkflowCallEventSecret(e.key, e.val)
			}
		case "outputs":
			ret.Outputs = map[string]*workflowlang.WorkflowCallEventOutput{}
			for e := range p.parseSectionMapping("outputs", e.val, true, false) {
				ret.Outputs[e.id] = p.parseWorkflowCallEventOutput(e.key, e.val)
			}
		default:
			p.unexpectedKey(e.key, "workflow_call", []string{"inputs", "secrets", "outputs"})
		}
	}

	return ret
}

func (p *Parser) parseImageVersionEvent(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.ImageVersionEvent {
	ret := &workflowlang.ImageVersionEvent{Pos: pos}

	for e := range p.parseSectionMapping("image_version", n, true, true) {
		switch e.id {
		case "types":
			ret.Types = p.parseStringOrStringSequence("types", e.val)
		case "names":
			ret.Names = p.parseStringOrStringSequence("names", e.val)
		case "versions":
			ret.Versions = p.parseStringOrStringSequence("versions", e.val)
		default:
			p.unexpectedKey(e.key, "image_version", []string{"types", "names", "versions"})
		}
	}

	return ret
}

func (p *Parser) parseEventWithNoConfig(n *syntax.Node) workflowlang.Event {
	s := p.parseString(n, false)
	switch s.Value {
	case "":
		return nil
	case "schedule":
		p.error(n, CodeTypeMismatch, "schedule event must be configured with mapping")
		return nil
	case "repository_dispatch":
		return &workflowlang.RepositoryDispatchEvent{Pos: PosAt(n)}
	case "workflow_dispatch":
		return &workflowlang.WorkflowDispatchEvent{Pos: PosAt(n)}
	case "workflow_call":
		return &workflowlang.WorkflowCallEvent{Pos: PosAt(n)}
	case "image_version":
		return &workflowlang.ImageVersionEvent{Pos: PosAt(n)}
	default:
		return &workflowlang.WebhookEvent{Hook: s, Pos: PosAt(n)}
	}
}

func (p *Parser) parseEvents(n *syntax.Node) []workflowlang.Event {
	switch n.Kind {
	case syntax.ScalarNode:
		if e := p.parseEventWithNoConfig(n); e != nil {
			return []workflowlang.Event{e}
		}
		return []workflowlang.Event{}
	case syntax.MappingNode:
		ret := []workflowlang.Event{}
		for e := range p.parseSectionMapping("on", n, false, true) {
			pos := e.key.Pos
			switch e.id {
			case "schedule":
				if e := p.parseScheduleEvent(pos, e.val); e != nil {
					ret = append(ret, e)
				}
			case "workflow_dispatch":
				ret = append(ret, p.parseWorkflowDispatchEvent(pos, e.val))
			case "repository_dispatch":
				ret = append(ret, p.parseRepositoryDispatchEvent(pos, e.val))
			case "workflow_call":
				ret = append(ret, p.parseWorkflowCallEvent(pos, e.val))
			case "image_version":
				ret = append(ret, p.parseImageVersionEvent(pos, e.val))
			default:
				ret = append(ret, p.parseWebhookEvent(e.key, e.val))
			}
		}

		return ret
	case syntax.SequenceNode:
		l := len(n.Content)
		p.checkNotEmpty("on", l, n)
		ret := make([]workflowlang.Event, 0, l)

		for _, c := range n.Content {
			if e := p.parseEventWithNoConfig(c); e != nil {
				ret = append(ret, e)
			}
		}

		return ret
	case syntax.DocumentNode, syntax.AliasNode:
	}
	p.typeErrorf(n, "\"on\" section value is expected to be mapping or sequence but found %s node", NodeKindName(n.Kind))
	return nil
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#permissions
func (p *Parser) parsePermissions(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Permissions {
	ret := &workflowlang.Permissions{Pos: pos}

	if n.Kind == syntax.ScalarNode {
		ret.All = p.parseString(n, false)
	} else {
		// XXX: Is the permission scope case insensitive?
		scopes := map[string]*workflowlang.PermissionScope{}
		for e := range p.parseSectionMapping("permissions", n, true, false) {
			scopes[e.id] = &workflowlang.PermissionScope{
				Name:  e.key,
				Value: p.parseString(e.val, false),
			}
		}
		ret.Scopes = scopes
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#env
func (p *Parser) parseEnv(n *syntax.Node) *workflowlang.Env {
	return p.parseEnvMapping(n, false)
}

func (p *Parser) parseEnvMapping(n *syntax.Node, allowEmpty bool) *workflowlang.Env {
	if n.Kind == syntax.ScalarNode {
		return &workflowlang.Env{
			Expression: p.parseExpression(n, "mapping value for \"env\" section"),
		}
	}

	env := &workflowlang.Env{Vars: map[string]*workflowlang.EnvVar{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "env"), n, allowEmpty, false, true) {
		if exprlang.IsMappingInsertion(e.key.Value) {
			env.Insertions = append(env.Insertions, p.parseEnvMapping(e.val, true))
			continue
		}
		env.Vars[e.id] = &workflowlang.EnvVar{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}

	return env
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#defaults
func (p *Parser) parseDefaults(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Defaults {
	ret := &workflowlang.Defaults{Pos: pos}

	for e := range p.parseSectionMapping("defaults", n, false, true) {
		if e.id != "run" {
			p.unexpectedKey(e.key, "defaults", []string{"run"})
			continue
		}
		ret.Run = &workflowlang.DefaultsRun{Pos: e.key.Pos}
		if expr := p.mayParseExpression(e.val); expr != nil {
			ret.Run.Expression = expr
			continue
		}

		for e := range p.parseSectionMapping("run", e.val, false, true) {
			switch e.id {
			case "shell":
				ret.Run.Shell = p.parseString(e.val, false)
			case "working-directory":
				ret.Run.WorkingDirectory = p.parseString(e.val, false)
			default:
				p.unexpectedKey(e.key, "run", []string{"shell", "working-directory"})
			}
		}
	}

	if ret.Run == nil {
		p.error(n, CodeMissingKey, "\"defaults\" section should have \"run\" section")
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idconcurrency
func (p *Parser) parseConcurrency(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Concurrency {
	ret := &workflowlang.Concurrency{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == syntax.ScalarNode {
		ret.Group = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping("concurrency", n, false, true) {
		switch e.id {
		case "group":
			ret.Group = p.parseString(e.val, false)
		case "cancel-in-progress":
			ret.CancelInProgress = p.parseBool(e.val)
		case "queue":
			if !p.CheckString(e.val, false) {
				continue
			}
			ret.Queue = newString(e.val)
			if ret.Queue.ContainsExpression() {
				continue
			}
			switch ret.Queue.Value {
			case "single", "max":
				// ok
			default:
				p.errorf(e.val, CodeInvalidValue, "invalid value %q for \"queue\" in \"concurrency\" section. valid values are \"single\" and \"max\"", ret.Queue.Value)
			}
		default:
			p.unexpectedKey(e.key, "concurrency", []string{"cancel-in-progress", "group", "queue"})
		}
	}
	if ret.Group == nil {
		p.errorAt(pos, CodeMissingKey, "group name is missing in \"concurrency\" section")
	}
	if ret.Queue != nil && ret.CancelInProgress != nil && ret.Queue.Value == "max" && ret.CancelInProgress.Expression == nil && ret.CancelInProgress.Value {
		p.errorfAt(ret.Queue.Pos, CodeConflictingKeys, "\"queue: max\" cannot be combined with \"cancel-in-progress: true\" in \"concurrency\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idenvironment
func (p *Parser) parseEnvironment(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Environment {
	ret := &workflowlang.Environment{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == syntax.ScalarNode {
		ret.Name = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping("environment", n, false, true) {
		switch e.id {
		case "name":
			ret.Name = p.parseString(e.val, false)
		case "url":
			ret.URL = p.parseString(e.val, false)
		case "deployment":
			ret.Deployment = p.parseBool(e.val)
		default:
			p.unexpectedKey(e.key, "environment", []string{"deployment", "name", "url"})
		}
	}
	if ret.Name == nil {
		p.errorAt(pos, CodeMissingKey, "name is missing in \"environment\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idoutputs
func (p *Parser) parseOutputs(n *syntax.Node) map[string]*workflowlang.Output {
	ret := map[string]*workflowlang.Output{}
	for e := range p.parseSectionMapping("outputs", n, false, false) {
		ret[e.id] = &workflowlang.Output{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}
	p.checkNotEmpty("outputs", len(ret), n)
	return ret
}

// GitHub reads an explicitly tagged scalar with the YAML 1.2 core schema and rejects a value the tag
// does not accept, as well as any tag other than "!!str" on a quoted or block scalar.
// https://github.com/actions/runner/blob/258d6c857db3519913f7deb6004b60172f8043ae/src/Sdk/WorkflowParser/Conversion/YamlObjectReader.cs#L36-L105
func (p *Parser) checkRawYAMLTag(n *syntax.Node) bool {
	if code, message := rawYAMLTagProblem(n); message != "" {
		p.error(n, code, message)
		return false
	}
	return true
}

// Check tags in values skipped by decoding because their mapping key is invalid.
func (p *Parser) checkRawYAMLTags(n *syntax.Node) {
	p.checkRawYAMLTag(n)
	for _, child := range n.Content {
		p.checkRawYAMLTags(child)
	}
}

func RawYAMLTagError(n *syntax.Node) string {
	_, message := rawYAMLTagProblem(n)
	return message
}

func rawYAMLTagProblem(n *syntax.Node) (Code, string) {
	// The reader marks a bare "!" without TaggedStyle. GitHub rejects it on a plain scalar.
	if n.Kind == syntax.ScalarNode && n.Tag == "!" && n.Style == 0 {
		return CodeUnknownTag, "the non-specific tag \"!\" is not supported by GitHub Actions; remove it or quote the value"
	}
	if n.Kind != syntax.ScalarNode || n.Style&syntax.TaggedStyle == 0 || n.Tag == workflowlang.YamlTagStr {
		return "", ""
	}
	if n.Style&(syntax.DoubleQuotedStyle|syntax.SingleQuotedStyle|syntax.LiteralStyle|syntax.FoldedStyle) != 0 {
		return CodeUnknownTag, fmt.Sprintf("tag of a quoted or block scalar must be \"!!str\" but got %q", n.Tag)
	}
	var ok bool
	switch n.Tag {
	case workflowlang.YamlTagBool:
		ok = schema.IsCoreSchemaBool(n.Value)
	case workflowlang.YamlTagNull:
		ok = schema.IsCoreSchemaNull(n.Value)
	case workflowlang.YamlTagInt:
		ok = schema.IsCoreSchemaInt(n.Value)
	case workflowlang.YamlTagFloat:
		ok = schema.IsCoreSchemaFloat(n.Value)
	default:
		return CodeUnknownTag, fmt.Sprintf("tag of a YAML scalar must be one of \"!!str\", \"!!bool\", \"!!int\", \"!!float\", \"!!null\" but got %q", n.Tag)
	}
	if !ok {
		return CodeTagValue, fmt.Sprintf("invalid value %q for %q tag", n.Value, n.Tag)
	}
	return "", ""
}

func (p *Parser) ParseRawYAMLValue(n *syntax.Node) workflowlang.RawYAMLValue {
	switch n.Kind {
	case syntax.ScalarNode:
		tag := n.Tag
		if !p.checkRawYAMLTag(n) {
			tag = workflowlang.YamlTagStr
		}
		return &workflowlang.RawYAMLString{Value: n.Value, Tag: tag, Position: PosAt(n)}
	case syntax.SequenceNode:
		vs := make([]workflowlang.RawYAMLValue, 0, len(n.Content))
		for _, c := range n.Content {
			if v := p.ParseRawYAMLValue(c); v != nil {
				vs = append(vs, v)
			}
		}
		return &workflowlang.RawYAMLArray{Elems: vs, Position: PosAt(n)}
	case syntax.MappingNode:
		m := map[string]workflowlang.RawYAMLValue{}
		for e := range p.parseMappingAt("matrix row value", n, true, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				m[e.id] = v
			}
		}
		return &workflowlang.RawYAMLObject{Props: m, Position: PosAt(n)}
	case syntax.DocumentNode, syntax.AliasNode:
	}
	p.typeErrorf(n, "unexpected %s node on parsing value in matrix row", NodeKindName(n.Kind))
	return nil
}

// ParseMatrixCombinations parses matrix include/exclude combinations.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-including-additional-values-into-combinations
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-excluding-configurations-from-a-matrix
func (p *Parser) ParseMatrixCombinations(sec string, n *syntax.Node) *workflowlang.MatrixCombinations {
	if n.Kind == syntax.ScalarNode {
		return &workflowlang.MatrixCombinations{
			Expression: p.parseExpression(n, "array of matrix combination"),
		}
	}

	if ok := p.checkSequence(sec, n, false); !ok {
		return nil
	}

	ret := make([]*workflowlang.MatrixCombination, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind == syntax.ScalarNode {
			if e := p.parseExpression(c, "mapping of matrix combination"); e != nil {
				ret = append(ret, &workflowlang.MatrixCombination{Expression: e})
			}
			continue
		}

		assigns := map[string]*workflowlang.MatrixAssign{}
		for e := range p.parseMapping(sprintf("element in %q section", sec), c, false, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				assigns[e.id] = &workflowlang.MatrixAssign{Key: e.key, Value: v}
			}
		}
		ret = append(ret, &workflowlang.MatrixCombination{Assigns: assigns})
	}
	return &workflowlang.MatrixCombinations{Combinations: ret}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
func (p *Parser) parseMatrix(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Matrix {
	return p.ParseMatrixMapping(pos, n, false, false)
}

func (p *Parser) ParseMatrixMapping(pos *workflowlang.Pos, n *syntax.Node, allowEmpty, evaluated bool) *workflowlang.Matrix {
	if n.Kind == syntax.ScalarNode {
		return &workflowlang.Matrix{
			Expression: p.parseExpression(n, "matrix"),
			Pos:        PosAt(n),
		}
	}

	ret := &workflowlang.Matrix{Pos: pos, Rows: make(map[string]*workflowlang.MatrixRow)}

	for e := range p.parseMappingEntries(sprintf("%q section", "matrix"), n, allowEmpty, false, !evaluated) {
		if !evaluated && exprlang.IsMappingInsertion(e.key.Value) {
			ret.Insertions = append(ret.Insertions, p.ParseMatrixMapping(e.key.Pos, e.val, true, false))
			continue
		}
		switch e.id {
		case "include":
			ret.Include = p.ParseMatrixCombinations("include", e.val)
			if ret.Include != nil {
				ret.Include.Name = e.key
				ret.Include.Evaluated = evaluated
			}
		case "exclude":
			ret.Exclude = p.ParseMatrixCombinations("exclude", e.val)
			if ret.Exclude != nil {
				ret.Exclude.Name = e.key
				ret.Exclude.Evaluated = evaluated
			}
		default:
			if e.val.Kind == syntax.ScalarNode {
				ret.Rows[e.id] = &workflowlang.MatrixRow{
					Name:       e.key,
					Expression: p.parseExpression(e.val, "array value for matrix variations"),
				}
				continue
			}

			if ok := p.checkSequence("matrix values", e.val, false); !ok {
				continue
			}

			values := make([]workflowlang.RawYAMLValue, 0, len(e.val.Content))
			for _, c := range e.val.Content {
				if v := p.ParseRawYAMLValue(c); v != nil {
					values = append(values, v)
				}
			}

			ret.Rows[e.id] = &workflowlang.MatrixRow{
				Name:   e.key,
				Values: values,
			}
		}
	}

	for _, row := range ret.Rows {
		row.Evaluated = evaluated
	}
	for _, filters := range []*workflowlang.MatrixCombinations{ret.Include, ret.Exclude} {
		if filters != nil {
			for _, combination := range filters.Combinations {
				combination.Evaluated = evaluated
			}
		}
	}
	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idstrategymax-parallel
func (p *Parser) parseMaxParallel(n *syntax.Node) *workflowlang.Int {
	i := p.parseInt(n)
	if i != nil && i.Expression == nil && i.Value <= 0 {
		p.errorf(n, CodeInvalidValue, "value at \"max-parallel\" must be greater than zero: %v", i.Value)
	}
	return i
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategy
func (p *Parser) parseStrategy(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Strategy {
	ret := &workflowlang.Strategy{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	for e := range p.parseSectionMapping("strategy", n, false, true) {
		switch e.id {
		case "matrix":
			ret.Matrix = p.parseMatrix(e.key.Pos, e.val)
		case "fail-fast":
			ret.FailFast = p.parseBool(e.val)
		case "max-parallel":
			ret.MaxParallel = p.parseMaxParallel(e.val)
		default:
			p.unexpectedKey(e.key, "strategy", []string{"matrix", "fail-fast", "max-parallel"})
		}
	}

	return ret
}

func (p *Parser) parseCredentials(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Credentials {
	ret := &workflowlang.Credentials{Pos: pos}

	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
		return ret
	}

	for e := range p.parseSectionMapping("credentials", n, false, true) {
		switch e.id {
		case "username":
			ret.Username = p.parseString(e.val, false)
		case "password":
			ret.Password = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "credentials", []string{"username", "password"})
		}
	}

	if ret.Username == nil || ret.Password == nil {
		p.errorAt(pos, CodeMissingKey, "both \"username\" and \"password\" must be specified in \"credentials\" section")
		return nil
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainer
func (p *Parser) parseContainer(sec string, pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Container {
	ret := &workflowlang.Container{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}
	keys := []string{
		"image",
		"credentials",
		"env",
		"ports",
		"volumes",
		"options",
	}
	if sec == "services" {
		keys = append(keys, "command", "entrypoint")
	}

	if n.Kind == syntax.ScalarNode {
		// When you only specify a container image, you can omit the image keyword.
		ret.Image = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping(sec, n, false, true) {
		switch e.id {
		case "image":
			// An empty image disables a service container.
			ret.Image = p.parseString(e.val, sec == "services")
		case "credentials":
			ret.Credentials = p.parseCredentials(e.key.Pos, e.val)
		case "env":
			ret.Env = p.parseEnv(e.val)
		case "ports":
			if expr := p.mayParseExpression(e.val); expr != nil {
				ret.PortsExpression = expr
			} else {
				ret.Ports = p.parseStringSequence("ports", e.val, true)
			}
		case "volumes":
			if expr := p.mayParseExpression(e.val); expr != nil {
				ret.VolumesExpression = expr
			} else {
				ret.Volumes = p.parseStringSequence("volumes", e.val, true)
			}
		case "options":
			ret.Options = p.parseString(e.val, true)
		case "command":
			if sec == "services" {
				ret.Command = p.parseString(e.val, true)
				continue
			}
			p.unexpectedKey(e.key, sec, keys)
		case "entrypoint":
			if sec == "services" {
				ret.Entrypoint = p.parseString(e.val, true)
				continue
			}
			p.unexpectedKey(e.key, sec, keys)
		default:
			p.unexpectedKey(e.key, sec, keys)
		}
	}

	if ret.Image == nil {
		p.errorfAt(pos, CodeMissingKey, "\"image\" is missing in %q section", sec)
	}

	return ret
}

func (p *Parser) parseServices(n *syntax.Node) *workflowlang.Services {
	ret := &workflowlang.Services{Pos: PosAt(n)}
	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
	} else {
		// XXX: Is the key case-insensitive?
		ss := map[string]*workflowlang.Service{}
		for e := range p.parseSectionMapping("services", n, false, false) {
			ss[e.id] = &workflowlang.Service{
				Name:      e.key,
				Container: p.parseContainer("services", e.key.Pos, e.val),
			}
		}
		ret.Value = ss
	}
	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
func (p *Parser) parseTimeoutMinutes(n *syntax.Node) *workflowlang.Float {
	f := p.parseFloat(n)
	if f != nil && f.Expression == nil && f.Value <= 0.0 {
		p.errorf(n, CodeInvalidValue, "value at \"timeout-minutes\" must be greater than zero: %v", f.Value)
	}
	return f
}

func (p *Parser) parseActionInputMapping(n *syntax.Node, allowEmpty bool, docker *workflowlang.ExecAction) *workflowlang.ActionInputMapping {
	if n.Kind == syntax.ScalarNode {
		expression := p.parseExpression(n, "mapping value for \"with\" section")
		p.parseDockerInputExpression(n, expression, docker)
		return &workflowlang.ActionInputMapping{Expression: expression}
	}
	mapping := &workflowlang.ActionInputMapping{Inputs: map[string]*workflowlang.Input{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "with"), n, allowEmpty, false, true) {
		switch {
		case exprlang.IsMappingInsertion(e.key.Value):
			mapping.Insertions = append(mapping.Insertions, p.parseActionInputMapping(e.val, true, docker))
		case p.parseDockerInput(e, docker):
		default:
			mapping.Inputs[e.id] = &workflowlang.Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
		}
	}
	return mapping
}

func (p *Parser) parseDockerInput(entry workflowMappingEntry, docker *workflowlang.ExecAction) bool {
	if docker == nil {
		return false
	}
	key, known := knownMappingKey(entry.key)
	if !known {
		return false
	}
	switch strings.ToLower(key.Value) {
	case "entrypoint":
		docker.Entrypoint = p.parseString(entry.val, false)
		docker.EntrypointEvaluated = false
	case "args":
		docker.Args = p.parseString(entry.val, true)
		docker.ArgsEvaluated = false
	default:
		return false
	}
	if entry.key.ContainsExpression() {
		docker.InputNameExpressions = append(docker.InputNameExpressions, entry.key)
	}
	return true
}

func (p *Parser) parseDockerInputExpression(n *syntax.Node, expression *workflowlang.String, docker *workflowlang.ExecAction) {
	if docker == nil || expression == nil {
		return
	}
	literal, known := workflowlang.WorkflowExpressionLiteral(expression)
	object, isObject := literal.(map[string]any)
	if !known || !isObject {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(object)) {
		key := strings.ToLower(name)
		if key != "entrypoint" && key != "args" {
			continue
		}
		value, scalar := workflowlang.WorkflowScalarString(object[name])
		if !scalar {
			continue
		}
		if key == "entrypoint" && value == "" {
			p.error(n, CodeEmptyValue, "string should not be empty")
			continue
		}
		resolved := &workflowlang.String{Value: value, Pos: expression.Pos}
		if key == "entrypoint" {
			docker.Entrypoint, docker.EntrypointEvaluated = resolved, true
		} else {
			docker.Args, docker.ArgsEvaluated = resolved, true
		}
	}
}

func (p *Parser) parseStepExecAction(entries []workflowMappingEntry, isDocker bool) *workflowlang.ExecAction {
	ret := &workflowlang.ExecAction{}

	for _, e := range entries {
		switch e.id {
		case "uses":
			if p.CheckString(e.val, false) {
				ret.Uses = p.parseStaticString(e.val)
			}
		case "with":
			if expr := p.mayParseExpression(e.val); expr != nil {
				ret.InputsExpression = expr
				if isDocker {
					p.parseDockerInputExpression(e.val, expr, ret)
				}
				continue
			}
			ret.Inputs = map[string]*workflowlang.Input{}
			with := p.parseMappingEntries(sprintf("%q section", "with"), e.val, false, false, true)
			if isDocker {
				for e := range with {
					if exprlang.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, ret))
						continue
					}
					if !p.parseDockerInput(e, ret) {
						ret.Inputs[e.id] = &workflowlang.Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
					}
				}
			} else {
				for e := range with {
					if exprlang.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, nil))
						continue
					}
					ret.Inputs[e.id] = &workflowlang.Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
				}
			}
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to execute action", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"background",
				"uses",
				"with",
			})
		}
	}

	return ret
}

func (p *Parser) parseStepExecRun(entries []workflowMappingEntry) *workflowlang.ExecRun {
	ret := &workflowlang.ExecRun{}

	for _, e := range entries {
		switch e.id {
		case "run":
			// Empty scripts keep their physical scalar and script occurrence;
			// ParseSteps diagnoses them for workflow steps only.
			ret.Run = p.parseString(e.val, true)
			ret.RunPos = e.key.Pos
			ret.Source = p.ScriptSourceForDeclaration(e.val, e.key.Pos)
		case "shell":
			ret.Shell = p.parseStaticString(e.val)
		case "working-directory":
			ret.WorkingDirectory = p.parseString(e.val, false)
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to run shell command", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"background",
				"run",
				"shell",
				"working-directory",
			})
		}
	}

	// Note: `ret.Run` is never `nil` because `ParseStep` checks `run` key in advance
	return ret
}

// parseStepExecWait parses a 'wait' or 'wait-all' step that waits for background steps to complete.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *Parser) parseStepExecWait(entries []workflowMappingEntry) *workflowlang.ExecWait {
	ret := &workflowlang.ExecWait{}
	waitGiven := false

	for _, e := range entries {
		switch e.id {
		case "wait":
			waitGiven = true
			ret.Names = p.parseStringOrStringSequence("wait", e.val)
		case "wait-all":
			// The schema allows null or a static boolean; false is diagnosed as a no-op.
			if e.val.Tag != workflowlang.YamlTagNull {
				if b := p.parseLiteralBool(e.val); b != nil && !b.Value {
					p.error(e.val, CodeInvalidValue, "the value of \"wait-all\" must be true or omitted")
				}
			}
			ret.All = true
			ret.AllPos = e.key.Pos
		case "id", "name", "continue-on-error":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to wait for background steps", []string{
				"id",
				"name",
				"continue-on-error",
				"wait",
				"wait-all",
			})
		}
	}

	// 'wait' and 'wait-all' are mutually exclusive: 'wait' targets specific
	// background steps while 'wait-all' waits for all of them.
	if waitGiven && ret.All {
		p.errorAt(ret.AllPos, CodeConflictingKeys, "\"wait\" and \"wait-all\" cannot be specified in the same step")
	}

	return ret
}

// parseStepExecCancel parses a 'cancel' step that cancels running background steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *Parser) parseStepExecCancel(entries []workflowMappingEntry) *workflowlang.ExecCancel {
	ret := &workflowlang.ExecCancel{}

	for _, e := range entries {
		switch e.id {
		case "cancel":
			// The 'cancel' step targets a single background step by its ID (not a list).
			ret.Name = p.parseString(e.val, false)
		case "id", "name", "continue-on-error":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to cancel background steps", []string{
				"id",
				"name",
				"continue-on-error",
				"cancel",
			})
		}
	}

	return ret
}

// parseStepExecParallel parses a 'parallel' step that runs a group of steps in parallel.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *Parser) parseStepExecParallel(entries []workflowMappingEntry) *workflowlang.ExecParallel {
	ret := &workflowlang.ExecParallel{}

	for _, e := range entries {
		switch e.id {
		case "parallel":
			ret.Steps = p.ParseSteps(e.val)
		default:
			p.unexpectedKey(e.key, "step to run steps in parallel", []string{
				"parallel",
			})
		}
	}

	return ret
}

// ParseStep parses a workflow or composite-action step.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
// ParseStepIsolated decodes one step and reports whether it decoded without
// syntax errors. The step's errors are not retained by the parser.
func (p *Parser) ParseStepIsolated(n *syntax.Node) (*workflowlang.Step, bool) {
	before := len(p.errs)
	step := p.ParseStep(n)
	clean := len(p.errs) == before
	p.errs = p.errs[:before]
	return step, clean
}

func (p *Parser) ParseStep(n *syntax.Node) *workflowlang.Step {
	ret := &workflowlang.Step{Pos: PosAt(n)}

	const (
		isUnknown = iota
		isAction
		isDocker
		isRun
		isWait
		isCancel
		isParallel
	)

	kind := isUnknown
	var declaredUses *workflowlang.String
	entries := slices.Collect(p.parseMappingAt("element of \"steps\" section", n, false, true))
	for _, e := range entries {
		switch e.id {
		case "id":
			ret.ID = p.parseStaticString(e.val)
		case "if":
			ret.If = p.parseCondition(e.val)
		case "name":
			ret.Name = p.parseString(e.val, true)
		case "env":
			ret.Env = p.parseEnv(e.val)
		case "continue-on-error":
			ret.ContinueOnError = p.parseBool(e.val)
		case "timeout-minutes":
			ret.TimeoutMinutes = p.parseTimeoutMinutes(e.val)
		case "background":
			ret.Background = p.parseBool(e.val)
		case "uses":
			uses := e.val.Value
			if literal := exprlang.LiteralExpressionValue(uses); literal != nil {
				uses = *literal
			}
			if e.val.Kind == syntax.ScalarNode {
				declaredUses = newString(e.val)
				declaredUses.Value = uses
			}
			if strings.HasPrefix(uses, "docker://") {
				kind = isDocker
			} else {
				kind = isAction
			}
		case "run":
			kind = isRun
			// Note: Unexpected keys are checked in parseStepExecAction or parseStepExecRun later
		case "wait", "wait-all":
			kind = isWait
		case "cancel":
			kind = isCancel
		case "parallel":
			kind = isParallel
			// Note: Unexpected keys are checked in the parseStepExec* functions later
		}
	}

	if kind != isAction && kind != isDocker {
		ret.DeclaredUses = declaredUses
	}
	switch kind {
	case isAction, isDocker:
		ret.Exec = p.parseStepExecAction(entries, kind == isDocker)
	case isRun:
		ret.Exec = p.parseStepExecRun(entries)
	case isWait:
		ret.Exec = p.parseStepExecWait(entries)
	case isCancel:
		ret.Exec = p.parseStepExecCancel(entries)
	case isParallel:
		ret.Exec = p.parseStepExecParallel(entries)
	default:
		p.error(n, CodeMissingKey, "step must run script with \"run\" section or run action with \"uses\" section")
	}

	return ret
}

// ParseSteps parses a sequence of workflow steps.
// An empty "run" script is diagnosed here rather than in ParseStep: composite
// action steps share ParseStep and treat any parse error as rejecting the
// step, while their metadata accepts an empty script.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
func (p *Parser) ParseSteps(n *syntax.Node) []*workflowlang.Step {
	if ok := p.checkSequence("steps", n, false); !ok {
		return nil
	}

	ret := make([]*workflowlang.Step, 0, len(n.Content))

	for _, c := range n.Content {
		if s := p.ParseStep(c); s != nil {
			// Only a run value that passed CheckString carries a source scalar,
			// so non-string values are not reported twice.
			if run, ok := s.Exec.(*workflowlang.ExecRun); ok && run.Run.SourceScalar.HasSource && run.Run.Value == "" {
				p.errorAt(run.Run.Pos, CodeEmptyValue, "string should not be empty")
			}
			ret = append(ret, s)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idruns-on
func (p *Parser) parseRunsOn(n *syntax.Node) *workflowlang.Runner {
	if expr := p.mayParseExpression(n); expr != nil {
		return &workflowlang.Runner{Expression: expr}
	}

	if n.Kind == syntax.ScalarNode || n.Kind == syntax.SequenceNode {
		labels := p.parseStringOrStringSequence("runs-on", n)
		return &workflowlang.Runner{Labels: labels}
	}

	r := &workflowlang.Runner{}
	for e := range p.parseSectionMapping("runs-on", n, false, true) {
		switch e.id {
		case "labels":
			if expr := p.mayParseExpression(e.val); expr != nil {
				r.LabelsExpr = expr
				continue
			}
			r.Labels = p.parseStringOrStringSequence("labels", e.val)
		case "group":
			r.Group = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "runs-on", []string{"labels", "group"})
		}
	}

	return r
}

func (p *Parser) parseSnapshot(pos *workflowlang.Pos, n *syntax.Node) *workflowlang.Snapshot {
	if expr := p.mayParseExpression(n); expr != nil {
		return &workflowlang.Snapshot{Expression: expr}
	}
	switch n.Kind {
	case syntax.ScalarNode:
		return &workflowlang.Snapshot{ImageName: p.parseString(n, false)}
	case syntax.MappingNode:
		ret := &workflowlang.Snapshot{}
		for e := range p.parseSectionMapping("on", n, false, true) {
			switch e.id {
			case "image-name":
				ret.ImageName = p.parseString(e.val, false)
			case "version":
				ret.Version = p.parseString(e.val, false)
			case "if":
				ret.If = p.parseCondition(e.val)
			default:
				p.unexpectedKey(e.key, "snapshot", []string{"image-name", "version", "if"})
			}
		}
		if ret.ImageName == nil {
			p.errorAt(pos, CodeMissingKey, "\"snapshot\" section must have \"image-name\" configuration")
		}
		return ret
	case syntax.DocumentNode, syntax.SequenceNode, syntax.AliasNode:
	}
	p.typeErrorf(n, "\"snapshot\" section value must be string or mapping but found %s node", NodeKindName(n.Kind))
	return nil
}

// JobKeyRequiresSteps is shared with file-based reusable workflow metadata.
// A job containing one of these keys cannot populate Job.WorkflowCall.
func JobKeyRequiresSteps(key string) bool {
	switch key {
	case "runs-on", "environment", "outputs", "env", "defaults", "steps", "timeout-minutes", "cancel-timeout-minutes", "continue-on-error", "container", "services", "snapshot":
		return true
	default:
		return false
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_id
func (p *Parser) parseJob(id *workflowlang.String, n *syntax.Node) *workflowlang.Job {
	ret := &workflowlang.Job{ID: id, Pos: id.Pos}
	call := &workflowlang.WorkflowCall{}

	// JobKeyRequiresSteps identifies keys excluded from reusable workflow calls.
	// https://docs.github.com/en/actions/using-workflows/reusing-workflows#supported-keywords-for-jobs-that-call-a-reusable-workflow
	var stepsOnlyKey *workflowlang.String
	var callOnlyKey *workflowlang.String

	for e := range p.parseMapping(sprintf("%q job", id.Value), n, false, true) {
		k, v := e.key, e.val
		if JobKeyRequiresSteps(e.id) {
			stepsOnlyKey = k
		}
		switch e.id {
		case "name":
			ret.Name = p.parseString(v, true)
		case "needs":
			if v.Kind == syntax.ScalarNode {
				// needs: job1
				ret.Needs = []*workflowlang.String{p.parseString(v, false)}
			} else {
				// needs: [job1, job2]
				ret.Needs = p.parseStringSequence("needs", v, false)
			}
		case "runs-on":
			ret.RunsOn = p.parseRunsOn(v)
		case "permissions":
			ret.Permissions = p.parsePermissions(k.Pos, v)
		case "cache-mode":
			ret.CacheMode = p.parseCacheMode(v)
		case "environment":
			ret.Environment = p.parseEnvironment(k.Pos, v)
		case "concurrency":
			ret.Concurrency = p.parseConcurrency(k.Pos, v)
		case "outputs":
			ret.Outputs = p.parseOutputs(v)
		case "env":
			ret.Env = p.parseEnv(v)
		case "defaults":
			ret.Defaults = p.parseDefaults(k.Pos, v)
		case "if":
			ret.If = p.parseCondition(v)
		case "steps":
			ret.Steps = p.ParseSteps(v)
		case "timeout-minutes":
			ret.TimeoutMinutes = p.parseTimeoutMinutes(v)
		case "cancel-timeout-minutes":
			ret.CancelTimeoutMinutes = p.parseFloat(v)
		case "strategy":
			ret.Strategy = p.parseStrategy(k.Pos, v)
		case "continue-on-error":
			ret.ContinueOnError = p.parseBool(v)
		case "container":
			ret.Container = p.parseContainer("container", k.Pos, v)
		case "services":
			ret.Services = p.parseServices(v)
		case "uses":
			call.Uses = p.parseStaticString(v)
			callOnlyKey = k
		case "with":
			call.Inputs = map[string]*workflowlang.WorkflowCallInput{}
			for e := range p.parseSectionMapping("with", v, false, false) {
				call.Inputs[e.id] = &workflowlang.WorkflowCallInput{
					Name:  e.key,
					Value: p.parseString(e.val, true),
				}
			}
			callOnlyKey = k
		case "secrets":
			if e.val.Kind == syntax.ScalarNode {
				// `secrets: inherit` special case
				// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_callsecretsinherit
				if e.val.Value == "inherit" {
					call.InheritSecrets = true
				} else {
					p.errorf(e.val, CodeTypeMismatch, "expected mapping node for secrets or \"inherit\" string node but found %q node", e.val.Value)
				}
			} else {
				call.Secrets = map[string]*workflowlang.WorkflowCallSecret{}
				for e := range p.parseSectionMapping("secrets", v, false, false) {
					call.Secrets[e.id] = &workflowlang.WorkflowCallSecret{
						Name:  e.key,
						Value: p.parseString(e.val, true),
					}
				}
			}
			callOnlyKey = k
		case "snapshot":
			ret.Snapshot = p.parseSnapshot(k.Pos, v)
		default:
			// this is inelegant as fuck
			// TODO: refactor this to use a map of valid keys instead of a switch statement
			// or summit'
			p.unexpectedKey(e.key, "job", []string{
				"name",
				"needs",
				"runs-on",
				"permissions",
				"cache-mode",
				"environment",
				"concurrency",
				"outputs",
				"env",
				"defaults",
				"if",
				"steps",
				"timeout-minutes",
				"cancel-timeout-minutes",
				"strategy",
				"continue-on-error",
				"container",
				"services",
				"uses",
				"with",
				"secrets",
				"snapshot",
			})
		}
	}

	if call.Uses != nil {
		if stepsOnlyKey != nil {
			ret.DeclaredUses = call.Uses
			p.errorfAt(
				stepsOnlyKey.Pos, CodeUnknownKey,
				"when a reusable workflow is called with \"uses\", %q is not available. only following keys are allowed: \"name\", \"uses\", \"with\", \"secrets\", \"needs\", \"if\", \"permissions\", \"cache-mode\", \"strategy\", and \"concurrency\" in job %q",
				stepsOnlyKey.Value,
				id.Value,
			)
		} else {
			ret.WorkflowCall = call
		}
	} else {
		// When not a reusable call
		if ret.Steps == nil {
			p.errorfAt(id.Pos, CodeMissingKey, "\"steps\" section is missing in job %q", id.Value)
		}
		if ret.RunsOn == nil {
			p.errorfAt(id.Pos, CodeMissingKey, "\"runs-on\" section is missing in job %q", id.Value)
		}
		if callOnlyKey != nil {
			p.errorfAt(
				callOnlyKey.Pos, CodeUnknownKey,
				"%q is only available for a reusable workflow call with \"uses\" but \"uses\" is not found in job %q",
				callOnlyKey.Value,
				id.Value,
			)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobs
func (p *Parser) parseJobs(n *syntax.Node) map[string]*workflowlang.Job {
	ret := map[string]*workflowlang.Job{}
	for e := range p.parseSectionMapping("jobs", n, false, false) {
		ret[e.id] = p.parseJob(e.key, e.val)
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions
// parse consumes an alias-projected document without changing its nodes.
func (p *Parser) parse(n *syntax.Node) *workflowlang.Workflow {
	w := &workflowlang.Workflow{}

	if len(n.Content) == 0 {
		position := PosAt(n)
		if position.Line == 0 {
			position.Line = 1
		}
		if position.Col == 0 {
			position.Col = 1
		}
		p.errorAt(position, CodeEmptyValue, "workflow is empty")
		return w
	}
	if root := n.Content[0]; root.Kind == syntax.MappingNode && len(root.Content) != 0 {
		w.Pos = PosAt(root.Content[0])
	}

	for e := range p.parseSectionMapping("workflow", n.Content[0], false, true) {
		k, v := e.key, e.val
		switch e.id {
		case "name":
			w.Name = p.parseString(v, true)
		case "description":
			w.Description = p.parseString(v, true)
		case "on":
			w.On = p.parseEvents(v)
		case "permissions":
			w.Permissions = p.parsePermissions(k.Pos, v)
		case "cache-mode":
			w.CacheMode = p.parseCacheMode(v)
		case "env":
			w.Env = p.parseEnv(v)
		case "defaults":
			w.Defaults = p.parseDefaults(k.Pos, v)
		case "concurrency":
			w.Concurrency = p.parseConcurrency(k.Pos, v)
		case "jobs":
			w.Jobs = p.parseJobs(v)
		case "run-name":
			w.RunName = p.parseString(v, false)
		default:
			p.unexpectedKey(k, "workflow", []string{
				"name",
				"description",
				"run-name",
				"on",
				"permissions",
				"cache-mode",
				"env",
				"defaults",
				"concurrency",
				"jobs",
			})
		}
	}

	if w.On == nil {
		p.error(n, CodeMissingKey, "\"on\" section is missing in workflow")
	}
	if w.Jobs == nil {
		p.error(n, CodeMissingKey, "\"jobs\" section is missing in workflow")
	}

	return w
}

// func dumpYAML(n *syntax.Node, level int) {
// 	fmt.Printf("%s%s (%s, %d,%d): %q\n", strings.Repeat(". ", level), NodeKindName(n.Kind), n.Tag, n.Line, n.Column, n.Value)
// 	for _, c := range n.Content {
// 		dumpYAML(c, level+1)
// 	}
// }

func HandleYAMLUnmarshalError(err error) []*SyntaxError {
	diagnostics := codec.LoadDiagnostics(err)
	findings := make([]*SyntaxError, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		findings = append(findings, &SyntaxError{
			Message: "could not parse as YAML: " + diagnostic.Message,
			Line:    diagnostic.Mark.Line,
			Column:  diagnostic.Mark.Column,
			Kind:    "syntax-check",
			Code:    CodeYAML,
		})
	}
	return findings
}

// Parse recovers workflow syntax and diagnostics. YAML load or expansion-admission
// failures return a nil workflow; recoverable syntax errors accompany the tree.
func Parse(b []byte) (*workflowlang.Workflow, []*SyntaxError) {
	n, err := codec.Parse(b)
	if err != nil {
		return nil, HandleYAMLUnmarshalError(err)
	}

	workflow, findings := parseDocument(b, n)
	return workflow, append(documentFindings(b), findings...)
}

// documentFindings reports stream-level input GitHub's workflow reader rejects.
// Action manifests use other loaders, which strip a byte order mark.
func documentFindings(b []byte) []*SyntaxError {
	var findings []*SyntaxError
	if bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		findings = append(findings, &SyntaxError{Message: "a byte order mark makes GitHub read the first key with a U+FEFF prefix; remove it", Line: 1, Column: 1, Kind: "syntax-check", Code: CodeBOM})
	}
	if mark, ok := codec.FollowingDocument(b); ok {
		findings = append(findings, &SyntaxError{Message: "GitHub Actions reads one YAML document; remove the document starting here", Line: mark.Line, Column: mark.Column, Kind: "syntax-check", Code: CodeMultipleDocuments})
	}
	return findings
}

// parseDocument owns one parser and one projection for the complete physical source.
func parseDocument(source []byte, root *syntax.Node) (*workflowlang.Workflow, []*SyntaxError) {
	p := NewSourceParser(source)
	p.IndexScalarSources(root)
	p.checkFlowMappingNulls(root, false)
	uses := inspectYAMLAliases(root, func(node *syntax.Node, _ YamlAliasDiagnostic, message string) {
		p.error(node, CodeAlias, message)
	})
	if err := admitYAMLAliasUses(root, uses); err != nil {
		return nil, append(p.errs, HandleYAMLUnmarshalError(err)...)
	}
	projected, aliases := projectYAMLAliases(root, uses)
	p.aliases = aliases
	workflow := p.parse(projected)
	workflow.ExpressionSources = p.workflowExpressionSources(projected)
	p.IndexDependencySpans(workflow)
	return workflow, p.errs
}

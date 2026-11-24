package parser

import (
	"bytes"
	"fmt"
	"iter"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func NodeKindName(k codec.NodeKind) string {
	switch k {
	case codec.DocumentNode:
		return "document"
	case codec.SequenceNode:
		return "sequence"
	case codec.MappingNode:
		return "mapping"
	case codec.ScalarNode:
		return "scalar"
	case codec.AliasNode:
		return "alias"
	default:
		panic(fmt.Sprintf("unreachable: unknown YAML kind: %v", k))
	}
}

func PosAt(n *codec.Node) *sourcetext.Position {
	return &sourcetext.Position{Line: n.Line, Column: n.Column}
}

func newString(n *codec.Node) *definition.String {
	quoted := n.Style&(codec.DoubleQuotedStyle|codec.SingleQuotedStyle) != 0
	switch n.Kind {
	case codec.DocumentNode, codec.SequenceNode, codec.MappingNode, codec.ScalarNode, codec.AliasNode:
	default:
		return &definition.String{Value: n.Value, Quoted: quoted, Pos: PosAt(n)}
	}
	pos := sourcetext.Position{Line: n.Line, Column: n.Column}
	return &definition.String{
		Value:  n.Value,
		Quoted: quoted,
		Pos:    &pos,
		SourceScalar: definition.SourceScalar{
			HasSource: true,
			Kind:      n.Kind,
			Tag:       n.Tag,
			Value:     n.Value,
			Pos:       pos,
		},
	}
}

type workflowMappingEntry struct {
	id  string
	key *definition.String
	val *codec.Node
}

type delayedSprintf struct {
	result string
	// An empty arg means the format string takes no argument.
	arg string
}

func sprintf(fmt, arg string) delayedSprintf {
	return delayedSprintf{fmt, arg}
}

func (l *delayedSprintf) String() string {
	if len(l.arg) > 0 {
		l.result = fmt.Sprintf(l.result, l.arg)
		l.arg = ""
	}
	return l.result
}

type Parser struct {
	scalarDocument       *sourcemap.Document
	errs                 []*SyntaxError
	sourceLines          []string
	aliases              map[*codec.Node]*codec.Node
	scalarContexts       map[sourcetext.Position]sourcemap.Context
	scalarSources        map[sourcetext.Position]*definition.ScriptSource
	scalarComments       map[sourcetext.Position]scalarCommentNodes
	scalarDeclarations   map[sourcetext.Position]scalarCommentNodes
	workflowCallFindings []*SyntaxError
}

func NewSourceParser(source []byte) *Parser {
	document := sourcemap.NewDocument(string(source))
	return &Parser{sourceLines: document.Lines(), scalarDocument: document}
}

func (p *Parser) error(n *codec.Node, code Code, m string) {
	p.errs = append(p.errs, &SyntaxError{Message: m, Position: sourcetext.Position{Line: n.Line, Column: n.Column}, Kind: "syntax-check", Code: code})
}

func (p *Parser) errorAt(pos *sourcetext.Position, code Code, m string) {
	p.errs = append(p.errs, &SyntaxError{Message: m, Position: *pos, Kind: "syntax-check", Code: code})
}

func (p *Parser) errorfAt(pos *sourcetext.Position, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.errorAt(pos, code, m)
}

func (p *Parser) errorf(n *codec.Node, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.error(n, code, m)
}

func (p *Parser) typeErrorf(n *codec.Node, format string, args ...any) {
	p.kindErrorf(n, CodeTypeMismatch, format, args...)
}

func (p *Parser) kindErrorf(n *codec.Node, code Code, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	if alias, ok := p.aliases[n]; ok {
		m += fmt.Sprintf(". alias %q refers to the anchor at line:%d, column:%d", alias.Value, n.Line, n.Column)
		n = alias
	}
	p.error(n, code, m)
}

func (p *Parser) unexpectedKey(s *definition.String, sec string, expected []string) {
	if !strings.ContainsRune(sec, ' ') {
		sec = fmt.Sprintf("%q section", sec)
	}
	var m string
	switch l := len(expected); {
	case l == 1:
		m = fmt.Sprintf("expected %q key for %s but got %q", expected[0], sec, s.Value)
	case l > 1:
		m = fmt.Sprintf("unexpected key %q for %s. expected one of %v", s.Value, sec, messages.SortedQuotes(expected))
	default:
		m = fmt.Sprintf("unexpected key %q for %s", s.Value, sec)
	}
	p.errorAt(s.Pos, CodeUnknownKey, m)
}

func (p *Parser) checkNotEmpty(sec string, count int, n *codec.Node) bool {
	if count == 0 {
		p.errorf(n, CodeEmptyValue, "%q section should not be empty", sec)
		return false
	}
	return true
}

func (p *Parser) checkSequence(sec string, n *codec.Node, allowEmpty bool) bool {
	if n.Kind != codec.SequenceNode {
		p.typeErrorf(n, "%q section must be sequence node but got %s node with %q tag", sec, NodeKindName(n.Kind), n.Tag)
		return false
	}
	return allowEmpty || p.checkNotEmpty(sec, len(n.Content), n)
}

func (p *Parser) CheckString(n *codec.Node, allowEmpty bool) bool {
	if n.Kind != codec.ScalarNode {
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

func (p *Parser) missingExpression(n *codec.Node, expecting string) {
	p.kindErrorf(n, CodeExpressionForm, "expecting a single ${{...}} expression or %s, but found plain text node", expecting)
}

func (p *Parser) parseExpression(n *codec.Node, expecting string) *definition.String {
	if !definition.IsExprAssigned(n.Value) {
		p.missingExpression(n, expecting)
		return nil
	}
	return newString(n)
}

func (p *Parser) mayParseExpression(n *codec.Node) *definition.String {
	if n.Tag != definition.YamlTagStr {
		return nil
	}
	if !definition.IsExprAssigned(n.Value) {
		return nil
	}
	return newString(n)
}

func (p *Parser) parseString(n *codec.Node, allowEmpty bool) *definition.String {
	if !p.CheckString(n, allowEmpty) {
		return &definition.String{Value: "", Quoted: false, Pos: PosAt(n)}
	}
	return newString(n)
}

func (p *Parser) parseCondition(n *codec.Node) *definition.String {
	s := p.parseString(n, true)
	switch n.Tag {
	case definition.YamlTagNull:
		s.Value = ""
	case definition.YamlTagBool:
		s.Value = strings.ToLower(s.Value)
	}
	if strings.TrimSpace(s.Value) == "" {
		s.Value = "success()"
	}
	return s
}

func (p *Parser) parseStaticString(n *codec.Node) *definition.String {
	if value := syntax.LiteralExpressionValue(n.Value); value != nil {
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

func (p *Parser) parseStringSequence(sec string, n *codec.Node, allowEmpty bool) []*definition.String {
	if ok := p.checkSequence(sec, n, allowEmpty); !ok {
		return nil
	}

	ss := make([]*definition.String, 0, len(n.Content))
	for _, c := range n.Content {
		s := p.parseString(c, false)
		if s != nil {
			ss = append(ss, s)
		}
	}
	return ss
}

func (p *Parser) parseStringOrStringSequence(sec string, n *codec.Node) []*definition.String {
	if n.Kind == codec.ScalarNode {
		return []*definition.String{p.parseString(n, false)}
	}
	return p.parseStringSequence(sec, n, false)
}

func (p *Parser) parseLiteralBool(n *codec.Node) *definition.Bool {
	if n.Kind != codec.ScalarNode || n.Tag != definition.YamlTagBool {
		p.typeErrorf(n, "expected boolean literal but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	return p.parseBool(n)
}

func (p *Parser) parseBool(n *codec.Node) *definition.Bool {
	if n.Kind != codec.ScalarNode || (n.Tag != definition.YamlTagBool && n.Tag != definition.YamlTagStr) {
		p.typeErrorf(n, "expected bool value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == definition.YamlTagStr {
		e := p.parseExpression(n, "boolean literal \"true\" or \"false\"")
		return &definition.Bool{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	return &definition.Bool{
		Value: strings.EqualFold(n.Value, "true"),
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseInt(n *codec.Node) *definition.Int {
	if n.Kind != codec.ScalarNode || (n.Tag != definition.YamlTagInt && n.Tag != definition.YamlTagFloat && n.Tag != definition.YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for integer value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == definition.YamlTagStr {
		e := p.parseExpression(n, "integer literal")
		if e == nil {
			return nil
		}
		return &definition.Int{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	if n.Tag == definition.YamlTagFloat {
		f, err := strconv.ParseFloat(n.Value, 64)
		if err != nil || math.IsNaN(f) || math.Trunc(f) < math.MinInt32 || math.Trunc(f) > math.MaxInt32 {
			p.errorf(n, CodeInvalidValue, "invalid integer value: %q", n.Value)
			return nil
		}
		return &definition.Int{
			Value:         int(math.Trunc(f)),
			TruncatedFrom: n.Value,
			Pos:           PosAt(n),
		}
	}

	i, err := parseYAMLInteger(n.Value)
	if err != nil {
		p.errorf(n, CodeInvalidValue, "invalid integer value: %q: %s", n.Value, err.Error())
		return nil
	}

	return &definition.Int{
		Value: i,
		Pos:   PosAt(n),
	}
}

func parseYAMLInteger(value string) (int, error) {
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0o") {
		return codec.CoreSchemaRadixInt(value)
	}
	return strconv.Atoi(value)
}

func (p *Parser) parseFloat(n *codec.Node) *definition.Float {
	if n.Kind != codec.ScalarNode || (n.Tag != definition.YamlTagFloat && n.Tag != definition.YamlTagInt && n.Tag != definition.YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for float value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == definition.YamlTagStr {
		e := p.parseExpression(n, "float number literal")
		if e == nil {
			return nil
		}
		return &definition.Float{
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

	return &definition.Float{
		Value: f,
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseMapping(where delayedSprintf, n *codec.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMappingEntries(where, n, allowEmpty, caseSensitive, false)
}

func (p *Parser) parseMappingEntries(where delayedSprintf, n *codec.Node, allowEmpty, caseSensitive, insertions bool) iter.Seq[workflowMappingEntry] {
	return func(yield func(workflowMappingEntry) bool) {
		if n.Kind == codec.ScalarNode && n.Tag == definition.YamlTagNull {
			if !allowEmpty {
				p.errorf(n, CodeEmptyValue, "%s should not be empty. please remove this section if it's unnecessary", where.String())
			}
			return
		}

		if n.Kind != codec.MappingNode {
			p.typeErrorf(n, "%s is %s node but mapping node is expected", where.String(), NodeKindName(n.Kind))
			return
		}

		keys := make(map[string]*sourcetext.Position, len(n.Content)/2)
		empty := true
		for i := 0; i < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			if insertions && keyNode.Kind == codec.ScalarNode {
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
			if insertions && emptyResolvedMappingKey(definition.SemanticValue(k)) {
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
				id = strings.ToLower(id)
			}

			if insertions && syntax.IsMappingInsertion(k.Value) {
				if !yield(workflowMappingEntry{id, k, n.Content[i+1]}) {
					break
				}
				inserted := resolveInsertedMapping(n.Content[i+1])
				insertedKeys := map[string]*definition.String{}
				for declaration := range inserted.Declarations() {
					switch declaration.State {
					case mappings.KnownKey:
						origin := declaration.Origin.Span.Start
						insertedKeys[strings.ToLower(declaration.Name)] = &definition.String{
							Value: declaration.Name,
							Pos:   &sourcetext.Position{Line: origin.Line, Column: origin.Column},
						}
					case mappings.InvalidKey:
						if n.Content[i+1].Kind == codec.ScalarNode && emptyResolvedMappingKey(declaration.Key) {
							origin := declaration.Origin.Span.Start
							pos := &sourcetext.Position{Line: origin.Line, Column: origin.Column}
							if origin.Line == 0 || origin.Column == 0 {
								pos = PosAt(n.Content[i+1])
							}
							p.errorAt(pos, CodeEmptyValue, "string should not be empty")
						}
					case mappings.UnknownKey:
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

func (p *Parser) parseSectionMapping(section string, n *codec.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf("%q section", section), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseMappingAt(where string, n *codec.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf(where, ""), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseScheduleEvent(pos *sourcetext.Position, n *codec.Node) *definition.ScheduledEvent {
	if ok := p.checkSequence("schedule", n, false); !ok {
		return nil
	}

	schedules := make([]*definition.ScheduleEntry, 0, len(n.Content))
	for _, c := range n.Content {
		entry := &definition.ScheduleEntry{}
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
		} else if !cronGiven && c.Kind == codec.MappingNode && len(c.Content) != 0 {
			p.error(c, CodeMissingKey, "\"cron\" is missing in element of \"schedule\" section")
		}
	}

	return &definition.ScheduledEvent{Schedules: schedules, Pos: pos}
}

func (p *Parser) parseWorkflowDispatchEventInput(name *definition.String, n *codec.Node) *definition.DispatchInput {
	ret := &definition.DispatchInput{Name: name}

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
				ret.Type = definition.WorkflowDispatchEventInputTypeString
			case "number":
				ret.Type = definition.WorkflowDispatchEventInputTypeNumber
			case "boolean":
				ret.Type = definition.WorkflowDispatchEventInputTypeBoolean
			case "choice":
				ret.Type = definition.WorkflowDispatchEventInputTypeChoice
			case "environment":
				ret.Type = definition.WorkflowDispatchEventInputTypeEnvironment
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

func (p *Parser) parseWorkflowDispatchEvent(pos *sourcetext.Position, n *codec.Node) *definition.WorkflowDispatchEvent {
	ret := &definition.WorkflowDispatchEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_dispatch", n, true, true) {
		if e.id != "inputs" {
			p.unexpectedKey(e.key, "workflow_dispatch", []string{"inputs"})
			continue
		}

		ret.Inputs = map[string]*definition.DispatchInput{}
		for e := range p.parseSectionMapping("inputs", e.val, true, false) {
			ret.Inputs[e.id] = p.parseWorkflowDispatchEventInput(e.key, e.val)
		}
	}

	return ret
}

func (p *Parser) parseRepositoryDispatchEvent(pos *sourcetext.Position, n *codec.Node) *definition.RepositoryDispatchEvent {
	ret := &definition.RepositoryDispatchEvent{Pos: pos}

	for e := range p.parseSectionMapping("repository_dispatch", n, true, true) {
		if e.id == "types" {
			ret.Types = p.parseStringOrStringSequence("types", e.val)
		} else {
			p.unexpectedKey(e.key, "repository_dispatch", []string{"types"})
		}
	}

	return ret
}

func (p *Parser) parseWebhookEventFilter(name *definition.String, n *codec.Node) *definition.WebhookEventFilter {
	v := p.parseStringOrStringSequence(name.Value, n)
	return &definition.WebhookEventFilter{Name: name, Values: v}
}

func (p *Parser) parseWebhookEvent(name *definition.String, n *codec.Node) *definition.WebhookEvent {
	ret := &definition.WebhookEvent{Hook: name, Pos: name.Pos}

	for e := range p.parseSectionMapping(name.Value, n, true, true) {
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

func (p *Parser) parseWorkflowCallEventInput(id string, name *definition.String, n *codec.Node) *definition.WorkflowCallEventInput {
	ret := &definition.WorkflowCallEventInput{Name: name, ID: id}
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
			var known bool
			ret.Type, known = definition.ParseWorkflowCallEventInputType(e.val.Value)
			if !known {
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

func (p *Parser) parseWorkflowCallEventSecret(name *definition.String, n *codec.Node) *definition.WorkflowCallEventSecret {
	ret := &definition.WorkflowCallEventSecret{Name: name}

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

func (p *Parser) parseWorkflowCallEventOutput(name *definition.String, n *codec.Node) *definition.WorkflowCallEventOutput {
	output := &definition.WorkflowCallEventOutput{Name: name}

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

func (p *Parser) parseWorkflowCallEvent(pos *sourcetext.Position, n *codec.Node) *definition.WorkflowCallEvent {
	ret := &definition.WorkflowCallEvent{Pos: pos}
	reported := len(p.errs)
	defer func() { p.workflowCallFindings = append(p.workflowCallFindings, p.errs[reported:]...) }()

	for e := range p.parseSectionMapping("workflow_call", n, true, true) {
		switch e.id {
		case "inputs":
			ret.Inputs = []*definition.WorkflowCallEventInput{}
			for e := range p.parseSectionMapping("inputs", e.val, true, false) {
				ret.Inputs = append(ret.Inputs, p.parseWorkflowCallEventInput(e.id, e.key, e.val))
			}
		case "secrets":
			ret.Secrets = map[string]*definition.WorkflowCallEventSecret{}
			for e := range p.parseSectionMapping("secrets", e.val, true, false) {
				ret.Secrets[e.id] = p.parseWorkflowCallEventSecret(e.key, e.val)
			}
		case "outputs":
			ret.Outputs = map[string]*definition.WorkflowCallEventOutput{}
			for e := range p.parseSectionMapping("outputs", e.val, true, false) {
				ret.Outputs[e.id] = p.parseWorkflowCallEventOutput(e.key, e.val)
			}
		default:
			p.unexpectedKey(e.key, "workflow_call", []string{"inputs", "secrets", "outputs"})
		}
	}

	return ret
}

func (p *Parser) parseImageVersionEvent(pos *sourcetext.Position, n *codec.Node) *definition.ImageVersionEvent {
	ret := &definition.ImageVersionEvent{Pos: pos}

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

func (p *Parser) parseEventWithNoConfig(n *codec.Node) definition.Event {
	s := p.parseString(n, false)
	switch s.Value {
	case "":
		return nil
	case "schedule":
		p.error(n, CodeTypeMismatch, "schedule event must be configured with mapping")
		return nil
	case "repository_dispatch":
		return &definition.RepositoryDispatchEvent{Pos: PosAt(n)}
	case "workflow_dispatch":
		return &definition.WorkflowDispatchEvent{Pos: PosAt(n)}
	case "workflow_call":
		return &definition.WorkflowCallEvent{Pos: PosAt(n)}
	case "image_version":
		return &definition.ImageVersionEvent{Pos: PosAt(n)}
	default:
		return &definition.WebhookEvent{Hook: s, Pos: PosAt(n)}
	}
}

func (p *Parser) parseEvents(n *codec.Node) []definition.Event {
	switch n.Kind {
	case codec.ScalarNode:
		// GitHub accepts an empty "on"; the empty-triggers rule reports it.
		if n.Tag == definition.YamlTagNull {
			return []definition.Event{}
		}
		if e := p.parseEventWithNoConfig(n); e != nil {
			return []definition.Event{e}
		}
		return nil
	case codec.MappingNode:
		ret := []definition.Event{}
		for e := range p.parseSectionMapping("on", n, true, true) {
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

		return declaredEvents(ret, n)
	case codec.SequenceNode:
		ret := make([]definition.Event, 0, len(n.Content))
		for _, c := range n.Content {
			if e := p.parseEventWithNoConfig(c); e != nil {
				ret = append(ret, e)
			}
		}

		return declaredEvents(ret, n)
	case codec.DocumentNode, codec.AliasNode:
	}
	p.typeErrorf(n, "\"on\" section value is expected to be mapping or sequence but found %s node", NodeKindName(n.Kind))
	return nil
}

func declaredEvents(events []definition.Event, n *codec.Node) []definition.Event {
	if len(events) == 0 && len(n.Content) != 0 {
		return nil
	}
	return events
}

func (p *Parser) parsePermissions(pos *sourcetext.Position, n *codec.Node) *definition.Permissions {
	ret := &definition.Permissions{Pos: pos}

	if n.Kind == codec.ScalarNode {
		ret.All = p.parseString(n, false)
	} else {
		scopes := map[string]*definition.PermissionScope{}
		for e := range p.parseSectionMapping("permissions", n, true, false) {
			scopes[e.id] = &definition.PermissionScope{
				Name:  e.key,
				Value: p.parseString(e.val, false),
			}
		}
		ret.Scopes = scopes
	}

	return ret
}

func (p *Parser) parseEnv(n *codec.Node) *definition.Env {
	return p.parseEnvMapping(n, false)
}

func (p *Parser) parseEnvMapping(n *codec.Node, allowEmpty bool) *definition.Env {
	if n.Kind == codec.ScalarNode {
		return &definition.Env{
			Expression: p.parseExpression(n, "mapping value for \"env\" section"),
		}
	}

	env := &definition.Env{Vars: map[string]*definition.EnvVar{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "env"), n, allowEmpty, false, true) {
		if syntax.IsMappingInsertion(e.key.Value) {
			env.Insertions = append(env.Insertions, p.parseEnvMapping(e.val, true))
			continue
		}
		env.Vars[e.id] = &definition.EnvVar{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}

	return env
}

func (p *Parser) parseDefaults(pos *sourcetext.Position, n *codec.Node) *definition.Defaults {
	ret := &definition.Defaults{Pos: pos}

	for e := range p.parseSectionMapping("defaults", n, false, true) {
		if e.id != "run" {
			p.unexpectedKey(e.key, "defaults", []string{"run"})
			continue
		}
		ret.Run = &definition.DefaultsRun{Pos: e.key.Pos}
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

func (p *Parser) parseConcurrency(pos *sourcetext.Position, n *codec.Node) *definition.Concurrency {
	ret := &definition.Concurrency{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == codec.ScalarNode {
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

func (p *Parser) parseEnvironment(pos *sourcetext.Position, n *codec.Node) *definition.Environment {
	ret := &definition.Environment{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == codec.ScalarNode {
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

func (p *Parser) parseOutputs(n *codec.Node) map[string]*definition.Output {
	ret := map[string]*definition.Output{}
	for e := range p.parseSectionMapping("outputs", n, false, false) {
		ret[e.id] = &definition.Output{
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
func (p *Parser) checkRawYAMLTag(n *codec.Node) bool {
	if code, message := rawYAMLTagProblem(n); message != "" {
		p.error(n, code, message)
		return false
	}
	return true
}

func (p *Parser) checkRawYAMLTags(n *codec.Node) {
	p.checkRawYAMLTag(n)
	for _, child := range n.Content {
		p.checkRawYAMLTags(child)
	}
}

func RawYAMLTagError(n *codec.Node) string {
	_, message := rawYAMLTagProblem(n)
	return message
}

func rawYAMLTagProblem(n *codec.Node) (Code, string) {
	// The reader marks a bare "!" without TaggedStyle. GitHub rejects it on a plain scalar.
	if n.Kind == codec.ScalarNode && n.Tag == "!" && n.Style == 0 {
		return CodeUnknownTag, "the non-specific tag \"!\" is not supported by GitHub Actions; remove it or quote the value"
	}
	if n.Kind != codec.ScalarNode || n.Style&codec.TaggedStyle == 0 || n.Tag == definition.YamlTagStr {
		return "", ""
	}
	if n.Style&(codec.DoubleQuotedStyle|codec.SingleQuotedStyle|codec.LiteralStyle|codec.FoldedStyle) != 0 {
		return CodeUnknownTag, fmt.Sprintf("tag of a quoted or block scalar must be \"!!str\" but got %q", n.Tag)
	}
	var ok bool
	switch n.Tag {
	case definition.YamlTagBool:
		ok = codec.IsCoreSchemaBool(n.Value)
	case definition.YamlTagNull:
		ok = codec.IsCoreSchemaNull(n.Value)
	case definition.YamlTagInt:
		ok = codec.IsCoreSchemaInt(n.Value)
	case definition.YamlTagFloat:
		ok = codec.IsCoreSchemaFloat(n.Value)
	default:
		return CodeUnknownTag, fmt.Sprintf("tag of a YAML scalar must be one of \"!!str\", \"!!bool\", \"!!int\", \"!!float\", \"!!null\" but got %q", n.Tag)
	}
	if !ok {
		return CodeTagValue, fmt.Sprintf("invalid value %q for %q tag", n.Value, n.Tag)
	}
	return "", ""
}

func (p *Parser) ParseRawYAMLValue(n *codec.Node) definition.RawYAMLValue {
	switch n.Kind {
	case codec.ScalarNode:
		tag := n.Tag
		if !p.checkRawYAMLTag(n) {
			tag = definition.YamlTagStr
		}
		return &definition.RawYAMLString{Value: n.Value, Tag: tag, Position: PosAt(n)}
	case codec.SequenceNode:
		vs := make([]definition.RawYAMLValue, 0, len(n.Content))
		for _, c := range n.Content {
			if v := p.ParseRawYAMLValue(c); v != nil {
				vs = append(vs, v)
			}
		}
		return &definition.RawYAMLArray{Elems: vs, Position: PosAt(n)}
	case codec.MappingNode:
		m := map[string]definition.RawYAMLValue{}
		for e := range p.parseMappingAt("matrix row value", n, true, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				m[e.id] = v
			}
		}
		return &definition.RawYAMLObject{Props: m, Position: PosAt(n)}
	case codec.DocumentNode, codec.AliasNode:
	}
	p.typeErrorf(n, "unexpected %s node on parsing value in matrix row", NodeKindName(n.Kind))
	return nil
}

func (p *Parser) ParseMatrixCombinations(sec string, n *codec.Node) *definition.MatrixCombinations {
	if n.Kind == codec.ScalarNode {
		return &definition.MatrixCombinations{
			Expression: p.parseExpression(n, "array of matrix combination"),
		}
	}

	if ok := p.checkSequence(sec, n, false); !ok {
		return nil
	}

	ret := make([]*definition.MatrixCombination, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind == codec.ScalarNode {
			if e := p.parseExpression(c, "mapping of matrix combination"); e != nil {
				ret = append(ret, &definition.MatrixCombination{Expression: e})
			}
			continue
		}

		assigns := map[string]*definition.MatrixAssign{}
		for e := range p.parseMapping(sprintf("element in %q section", sec), c, false, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				assigns[e.id] = &definition.MatrixAssign{Key: e.key, Value: v}
			}
		}
		ret = append(ret, &definition.MatrixCombination{Assigns: assigns})
	}
	return &definition.MatrixCombinations{Combinations: ret}
}

func (p *Parser) parseMatrix(pos *sourcetext.Position, n *codec.Node) *definition.Matrix {
	return p.ParseMatrixMapping(pos, n, false, false)
}

func (p *Parser) ParseMatrixMapping(pos *sourcetext.Position, n *codec.Node, allowEmpty, evaluated bool) *definition.Matrix {
	if n.Kind == codec.ScalarNode {
		return &definition.Matrix{
			Expression: p.parseExpression(n, "matrix"),
			Pos:        PosAt(n),
		}
	}

	ret := &definition.Matrix{Pos: pos, Rows: make(map[string]*definition.MatrixRow)}

	for e := range p.parseMappingEntries(sprintf("%q section", "matrix"), n, allowEmpty, false, !evaluated) {
		if !evaluated && syntax.IsMappingInsertion(e.key.Value) {
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
			if e.val.Kind == codec.ScalarNode {
				ret.Rows[e.id] = &definition.MatrixRow{
					Name:       e.key,
					Expression: p.parseExpression(e.val, "array value for matrix variations"),
				}
				continue
			}

			if ok := p.checkSequence("matrix values", e.val, false); !ok {
				continue
			}

			values := make([]definition.RawYAMLValue, 0, len(e.val.Content))
			for _, c := range e.val.Content {
				if v := p.ParseRawYAMLValue(c); v != nil {
					values = append(values, v)
				}
			}

			ret.Rows[e.id] = &definition.MatrixRow{
				Name:   e.key,
				Values: values,
			}
		}
	}

	for _, row := range ret.Rows {
		row.Evaluated = evaluated
	}
	for _, filters := range []*definition.MatrixCombinations{ret.Include, ret.Exclude} {
		if filters != nil {
			for _, combination := range filters.Combinations {
				combination.Evaluated = evaluated
			}
		}
	}
	return ret
}

func (p *Parser) parseStrategy(pos *sourcetext.Position, n *codec.Node) *definition.Strategy {
	ret := &definition.Strategy{Pos: pos}
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
			ret.MaxParallel = p.parseInt(e.val)
		default:
			p.unexpectedKey(e.key, "strategy", []string{"matrix", "fail-fast", "max-parallel"})
		}
	}

	return ret
}

func (p *Parser) parseCredentials(pos *sourcetext.Position, n *codec.Node) *definition.Credentials {
	ret := &definition.Credentials{Pos: pos}

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

func (p *Parser) parseContainer(sec string, pos *sourcetext.Position, n *codec.Node) *definition.Container {
	ret := &definition.Container{Pos: pos}
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

	if n.Kind == codec.ScalarNode {
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

func (p *Parser) parseServices(n *codec.Node) *definition.Services {
	ret := &definition.Services{Pos: PosAt(n)}
	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
	} else {
		ss := map[string]*definition.Service{}
		for e := range p.parseSectionMapping("services", n, false, false) {
			ss[e.id] = &definition.Service{
				Name:      e.key,
				Container: p.parseContainer("services", e.key.Pos, e.val),
			}
		}
		ret.Value = ss
	}
	return ret
}

func (p *Parser) parseTimeoutMinutes(n *codec.Node) *definition.Float {
	f := p.parseFloat(n)
	if f != nil && f.Expression == nil && f.Value <= 0.0 {
		p.errorf(n, CodeInvalidValue, "value at \"timeout-minutes\" must be greater than zero: %v", f.Value)
	}
	return f
}

func (p *Parser) parseActionInputMapping(n *codec.Node, allowEmpty bool, docker *definition.ExecAction) *definition.ActionInputMapping {
	if n.Kind == codec.ScalarNode {
		expression := p.parseExpression(n, "mapping value for \"with\" section")
		p.parseDockerInputExpression(n, expression, docker)
		return &definition.ActionInputMapping{Expression: expression}
	}
	mapping := &definition.ActionInputMapping{Inputs: map[string]*definition.ActionArgument{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "with"), n, allowEmpty, false, true) {
		switch {
		case syntax.IsMappingInsertion(e.key.Value):
			mapping.Insertions = append(mapping.Insertions, p.parseActionInputMapping(e.val, true, docker))
		case p.parseDockerInput(e, docker):
		default:
			mapping.Inputs[e.id] = &definition.ActionArgument{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
		}
	}
	return mapping
}

func (p *Parser) parseDockerInput(entry workflowMappingEntry, docker *definition.ExecAction) bool {
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

func (p *Parser) parseDockerInputExpression(n *codec.Node, expression *definition.String, docker *definition.ExecAction) {
	if docker == nil || expression == nil {
		return
	}
	literal, known := definition.WorkflowExpressionLiteral(expression)
	object, isObject := literal.(map[string]any)
	if !known || !isObject {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(object)) {
		key := strings.ToLower(name)
		if key != "entrypoint" && key != "args" {
			continue
		}
		value, scalar := definition.WorkflowScalarString(object[name])
		if !scalar {
			continue
		}
		if key == "entrypoint" && value == "" {
			p.error(n, CodeEmptyValue, "string should not be empty")
			continue
		}
		resolved := &definition.String{Value: value, Pos: expression.Pos}
		if key == "entrypoint" {
			docker.Entrypoint, docker.EntrypointEvaluated = resolved, true
		} else {
			docker.Args, docker.ArgsEvaluated = resolved, true
		}
	}
}

func (p *Parser) parseStepExecAction(entries []workflowMappingEntry, isDocker bool) *definition.ExecAction {
	ret := &definition.ExecAction{}

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
			ret.Inputs = map[string]*definition.ActionArgument{}
			with := p.parseMappingEntries(sprintf("%q section", "with"), e.val, false, false, true)
			if isDocker {
				for e := range with {
					if syntax.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, ret))
						continue
					}
					if !p.parseDockerInput(e, ret) {
						ret.Inputs[e.id] = &definition.ActionArgument{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
					}
				}
			} else {
				for e := range with {
					if syntax.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, nil))
						continue
					}
					ret.Inputs[e.id] = &definition.ActionArgument{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
				}
			}
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
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

func (p *Parser) parseStepExecRun(entries []workflowMappingEntry) *definition.ExecRun {
	ret := &definition.ExecRun{}

	for _, e := range entries {
		switch e.id {
		case "run":
			// GitHub accepts an empty script and the empty-run rule reports it.
			ret.Run = p.parseString(e.val, true)
			ret.RunPos = e.key.Pos
			ret.Source = p.ScriptSourceForDeclaration(e.val, e.key.Pos)
		case "shell":
			ret.Shell = p.parseStaticString(e.val)
		case "working-directory":
			ret.WorkingDirectory = p.parseString(e.val, false)
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
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

	return ret
}

func (p *Parser) parseStepExecWait(entries []workflowMappingEntry) *definition.ExecWait {
	ret := &definition.ExecWait{}
	waitGiven := false

	for _, e := range entries {
		switch e.id {
		case "wait":
			waitGiven = true
			ret.Names = p.parseStringOrStringSequence("wait", e.val)
		case "wait-all":
			if e.val.Tag != definition.YamlTagNull {
				if b := p.parseLiteralBool(e.val); b != nil && !b.Value {
					p.error(e.val, CodeInvalidValue, "the value of \"wait-all\" must be true or omitted")
				}
			}
			ret.All = true
			ret.AllPos = e.key.Pos
		case "id", "name", "continue-on-error":
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

	if waitGiven && ret.All {
		p.errorAt(ret.AllPos, CodeConflictingKeys, "\"wait\" and \"wait-all\" cannot be specified in the same step")
	}

	return ret
}

func (p *Parser) parseStepExecCancel(entries []workflowMappingEntry) *definition.ExecCancel {
	ret := &definition.ExecCancel{}

	for _, e := range entries {
		switch e.id {
		case "cancel":
			ret.Name = p.parseString(e.val, false)
		case "id", "name", "continue-on-error":
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

func (p *Parser) parseStepExecParallel(entries []workflowMappingEntry) *definition.ExecParallel {
	ret := &definition.ExecParallel{}

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

// ParseStepIsolated decodes one step, discards its syntax errors, and reports whether it had none.
func (p *Parser) ParseStepIsolated(n *codec.Node) (*definition.Step, bool) {
	before := len(p.errs)
	step := p.ParseStep(n)
	clean := len(p.errs) == before
	p.errs = p.errs[:before]
	return step, clean
}

func (p *Parser) ParseStep(n *codec.Node) *definition.Step {
	ret := &definition.Step{Pos: PosAt(n)}

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
	var declaredUses *definition.String
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
			if literal := syntax.LiteralExpressionValue(uses); literal != nil {
				uses = *literal
			}
			if e.val.Kind == codec.ScalarNode {
				declaredUses = newString(e.val)
				declaredUses.Value = uses
			}
			if callreferences.FormOf(uses) == callreferences.FormContainer {
				kind = isDocker
			} else {
				kind = isAction
			}
		case "run":
			kind = isRun
		case "wait", "wait-all":
			kind = isWait
		case "cancel":
			kind = isCancel
		case "parallel":
			kind = isParallel
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

func (p *Parser) ParseSteps(n *codec.Node) []*definition.Step {
	// The empty-job rule reports jobs without steps or a reusable workflow call.
	if ok := p.checkSequence("steps", n, true); !ok {
		return nil
	}

	ret := make([]*definition.Step, 0, len(n.Content))

	for _, c := range n.Content {
		if s := p.ParseStep(c); s != nil {
			ret = append(ret, s)
		}
	}

	return ret
}

func (p *Parser) parseRunsOn(n *codec.Node) *definition.Runner {
	if expr := p.mayParseExpression(n); expr != nil {
		return &definition.Runner{Expression: expr}
	}

	if n.Kind == codec.ScalarNode || n.Kind == codec.SequenceNode {
		labels := p.parseStringOrStringSequence("runs-on", n)
		return &definition.Runner{Labels: labels}
	}

	r := &definition.Runner{}
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

func (p *Parser) parseSnapshot(pos *sourcetext.Position, n *codec.Node) *definition.Snapshot {
	if expr := p.mayParseExpression(n); expr != nil {
		return &definition.Snapshot{Expression: expr}
	}
	switch n.Kind {
	case codec.ScalarNode:
		return &definition.Snapshot{ImageName: p.parseString(n, false)}
	case codec.MappingNode:
		ret := &definition.Snapshot{}
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
	case codec.DocumentNode, codec.SequenceNode, codec.AliasNode:
	}
	p.typeErrorf(n, "\"snapshot\" section value must be string or mapping but found %s node", NodeKindName(n.Kind))
	return nil
}

func JobKeyRequiresSteps(key string) bool {
	switch key {
	case "runs-on", "environment", "outputs", "env", "defaults", "steps", "timeout-minutes", "cancel-timeout-minutes", "continue-on-error", "container", "services", "snapshot":
		return true
	default:
		return false
	}
}

func (p *Parser) parseJob(id *definition.String, n *codec.Node) *definition.Job {
	ret := &definition.Job{ID: id, Pos: id.Pos}
	call := &definition.WorkflowCall{}

	var stepsOnlyKey *definition.String
	var callOnlyKey *definition.String

	for e := range p.parseMapping(sprintf("%q job", id.Value), n, false, true) {
		k, v := e.key, e.val
		if JobKeyRequiresSteps(e.id) {
			stepsOnlyKey = k
		}
		switch e.id {
		case "name":
			ret.Name = p.parseString(v, true)
		case "needs":
			if v.Kind == codec.ScalarNode {
				ret.Needs = []*definition.String{p.parseString(v, false)}
			} else {
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
			call.Inputs = map[string]*definition.WorkflowCallInput{}
			for e := range p.parseSectionMapping("with", v, false, false) {
				call.Inputs[e.id] = &definition.WorkflowCallInput{
					Name:  e.key,
					Value: p.parseString(e.val, true),
				}
			}
			callOnlyKey = k
		case "secrets":
			if e.val.Kind == codec.ScalarNode {
				if e.val.Value == "inherit" {
					call.InheritSecrets = true
				} else {
					p.errorf(e.val, CodeTypeMismatch, "expected mapping node for secrets or \"inherit\" string node but found %q node", e.val.Value)
				}
			} else {
				call.Secrets = map[string]*definition.WorkflowCallSecret{}
				for e := range p.parseSectionMapping("secrets", v, false, false) {
					call.Secrets[e.id] = &definition.WorkflowCallSecret{
						Name:  e.key,
						Value: p.parseString(e.val, true),
					}
				}
			}
			callOnlyKey = k
		case "snapshot":
			ret.Snapshot = p.parseSnapshot(k.Pos, v)
		default:
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

func (p *Parser) parseJobs(n *codec.Node) map[string]*definition.Job {
	ret := map[string]*definition.Job{}
	for e := range p.parseSectionMapping("jobs", n, false, false) {
		ret[e.id] = p.parseJob(e.key, e.val)
	}
	return ret
}

func (p *Parser) parse(n *codec.Node) *definition.Workflow {
	w := &definition.Workflow{}

	if len(n.Content) == 0 {
		position := PosAt(n)
		if position.Line == 0 {
			position.Line = 1
		}
		if position.Column == 0 {
			position.Column = 1
		}
		p.errorAt(position, CodeEmptyValue, "workflow is empty")
		return w
	}
	if root := n.Content[0]; root.Kind == codec.MappingNode && len(root.Content) != 0 {
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
			w.OnPos = k.Pos
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

	if w.OnPos == nil {
		p.error(n, CodeMissingKey, "\"on\" section is missing in workflow")
	}
	if w.Jobs == nil {
		p.error(n, CodeMissingKey, "\"jobs\" section is missing in workflow")
	}

	return w
}

func HandleYAMLUnmarshalError(err error) []*SyntaxError {
	diagnostics := codec.LoadDiagnostics(err)
	findings := make([]*SyntaxError, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		findings = append(findings, &SyntaxError{
			Message:  "could not parse as YAML: " + diagnostic.Message(),
			Position: sourcetext.Position{Line: diagnostic.Mark().Line, Column: diagnostic.Mark().Column},
			Kind:     "syntax-check",
			Code:     CodeYAML,
		})
	}
	return findings
}

func ParseWorkflow(b []byte) (*definition.Workflow, []*SyntaxError) {
	n, err := codec.ParseYAML(b)
	if err != nil {
		return nil, HandleYAMLUnmarshalError(err)
	}

	workflow, findings := parseDocument(b, n)
	return workflow, append(documentFindings(b), findings...)
}

func ParseReusableWorkflow(b []byte) (workflow *definition.Workflow, findings, workflowCallFindings []*SyntaxError) {
	n, err := codec.ParseYAML(b)
	if err != nil {
		return nil, HandleYAMLUnmarshalError(err), nil
	}
	workflow, p := runDocumentParser(b, n)
	return workflow, append(documentFindings(b), p.errs...), p.workflowCallFindings
}

func documentFindings(b []byte) []*SyntaxError {
	var findings []*SyntaxError
	if bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		findings = append(findings, &SyntaxError{Message: "a byte order mark makes GitHub read the first key with a U+FEFF prefix; remove it", Position: sourcetext.Position{Line: 1, Column: 1}, Kind: "syntax-check", Code: CodeBOM})
	}
	if mark, ok := codec.FollowingDocument(b); ok {
		findings = append(findings, &SyntaxError{Message: "GitHub Actions reads one YAML document; remove the document starting here", Position: sourcetext.Position{Line: mark.Line, Column: mark.Column}, Kind: "syntax-check", Code: CodeMultipleDocuments})
	}
	return findings
}

func parseDocument(source []byte, root *codec.Node) (*definition.Workflow, []*SyntaxError) {
	workflow, p := runDocumentParser(source, root)
	return workflow, p.errs
}

func runDocumentParser(source []byte, root *codec.Node) (*definition.Workflow, *Parser) {
	p := NewSourceParser(source)
	p.IndexScalarSources(root)
	p.checkFlowMappingNulls(root, false)
	uses := inspectYAMLAliases(root, func(node *codec.Node, _ YamlAliasDiagnostic, message string) {
		p.error(node, CodeAlias, message)
	})
	if err := admitYAMLAliasUses(root, uses); err != nil {
		p.errs = append(p.errs, HandleYAMLUnmarshalError(err)...)
		return nil, p
	}
	projected, aliases := projectYAMLAliases(root, uses)
	p.aliases = aliases
	workflow := p.parse(projected)
	workflow.ExpressionSources = p.ExpressionSources(projected)
	p.IndexDependencySpans(workflow)
	return workflow, p
}

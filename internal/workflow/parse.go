package workflow

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	stringutil "actionlint.kjanat.dev/v2/internal/stringutil"
	"actionlint.kjanat.dev/v2/internal/yaml"
	yamlscalar "actionlint.kjanat.dev/v2/internal/yamlscalar"
	"actionlint.kjanat.dev/v2/internal/yamlsource"
)

const (
	YamlTagStr   = "!!str"
	YamlTagBool  = "!!bool"
	YamlTagInt   = "!!int"
	YamlTagFloat = "!!float"
	YamlTagNull  = "!!null"
)

// NodeKindName describes the kind of a YAML node.
// https://pkg.go.dev/go.yaml.in/yaml/v4#Kind
func NodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias" // Should be unreachable because we resolve all aliases before parsing
	default:
		panic(fmt.Sprintf("unreachable: unknown YAML kind: %v", k))
	}
}

func PosAt(n *yaml.Node) *Pos {
	return &Pos{n.Line, n.Column}
}

func newString(n *yaml.Node) *String {
	quoted := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0
	return &String{n.Value, quoted, PosAt(n)}
}

// workflowMappingEntry represents a key-value entry in YAML mapping.
type workflowMappingEntry struct {
	// id is a key in lower case for comparing case-insensitive keys.
	id  string
	key *String
	val *yaml.Node
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
	ScalarDocument     *yamlsource.Document
	Errors             []*SyntaxError
	SourceLines        []string
	aliases            map[*yaml.Node]*yaml.Node
	scalarContexts     map[Pos]scalarContext
	scalarSources      map[Pos]*ScriptSource
	scalarComments     map[Pos]scalarCommentNodes
	scalarDeclarations map[Pos]scalarCommentNodes
}

func SplitSourceLines(b []byte) []string {
	return yamlsource.NewDocument(string(b)).Lines
}

func NewSourceParser(source []byte) *Parser {
	document := yamlsource.NewDocument(string(source))
	return &Parser{SourceLines: document.Lines, ScalarDocument: document}
}

func (p *Parser) error(n *yaml.Node, m string) {
	p.Errors = append(p.Errors, &SyntaxError{Message: m, Line: n.Line, Column: n.Column, Kind: "syntax-check"})
}

func (p *Parser) errorAt(pos *Pos, m string) {
	p.Errors = append(p.Errors, &SyntaxError{Message: m, Line: pos.Line, Column: pos.Col, Kind: "syntax-check"})
}

func (p *Parser) errorfAt(pos *Pos, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.errorAt(pos, m)
}

func (p *Parser) errorf(n *yaml.Node, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	p.error(n, m)
}

func (p *Parser) typeErrorf(n *yaml.Node, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	if alias, ok := p.aliases[n]; ok {
		m += fmt.Sprintf(". alias %q refers to the anchor at line:%d, column:%d", alias.Value, n.Line, n.Column)
		n = alias
	}
	p.error(n, m)
}

func (p *Parser) resolveAliases(root *yaml.Node) {
	p.aliases = ResolveYAMLAliases(root, func(n *yaml.Node, d YamlAliasDiagnostic, m string) {
		p.error(n, m)
	})
}

// YamlAliasDiagnostic is the kind of a problem ResolveYAMLAliases reports. A recursive alias leaves
// an unresolved alias node in the tree, so decoding the tree afterwards is only safe when no such
// diagnostic was reported. An unused anchor leaves the tree fully resolved.
type YamlAliasDiagnostic int

const (
	yamlAliasDiagnosticRecursive YamlAliasDiagnostic = iota
	YamlAliasDiagnosticUnusedAnchor
	yamlAliasDiagnosticInvalidName
)

func ResolveYAMLAliases(root *yaml.Node, report func(n *yaml.Node, d YamlAliasDiagnostic, m string)) map[*yaml.Node]*yaml.Node {
	type usage struct {
		used    bool
		defined bool
	}

	anchors := map[*yaml.Node]*usage{}
	var aliases map[*yaml.Node]*yaml.Node

	var resolve func(*yaml.Node) // For recursive call
	resolve = func(n *yaml.Node) {
		var u *usage
		if len(n.Anchor) != 0 {
			if strings.ContainsRune(n.Anchor, '+') {
				report(n, yamlAliasDiagnosticInvalidName, fmt.Sprintf("anchor name %q contains '+' which is not allowed by GitHub Actions", n.Anchor))
			}
			u = &usage{}
			anchors[n] = u
		}
		for i, c := range n.Content {
			if c.Kind != yaml.AliasNode {
				resolve(c)
				continue
			}
			if strings.ContainsRune(c.Value, '+') {
				report(c, yamlAliasDiagnosticInvalidName, fmt.Sprintf("alias name %q contains '+' which is not allowed by GitHub Actions", c.Value))
			}
			// Note: Unknown anchors are detected by go-yaml Parser so we don't need to detect them by ourselves.
			if u, ok := anchors[c.Alias]; ok {
				u.used = true
				if u.defined {
					// Keep each alias use distinct without relocating the anchored content.
					resolved := *c.Alias
					n.Content[i] = &resolved
					if aliases == nil {
						aliases = map[*yaml.Node]*yaml.Node{}
					}
					aliases[&resolved] = c
				} else {
					// Don't resolve the recursive alias because it causes stack overflow on parsing the tree as
					// `RawYAMLValue`. (#610)
					report(c, yamlAliasDiagnosticRecursive, fmt.Sprintf("recursive alias %q is found. anchor was declared at line:%d, column:%d", c.Alias.Anchor, c.Alias.Line, c.Alias.Column))
				}
			}
		}
		if u != nil {
			u.defined = true
		}
	}
	resolve(root)

	for n, u := range anchors {
		if !u.used {
			report(n, YamlAliasDiagnosticUnusedAnchor, fmt.Sprintf("anchor %q is defined but not used", n.Anchor))
		}
	}
	return aliases
}

func (p *Parser) unexpectedKey(s *String, sec string, expected []string) {
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
	p.errorAt(s.Pos, m)
}

func (p *Parser) checkNotEmpty(sec string, count int, n *yaml.Node) bool {
	if count == 0 {
		p.errorf(n, "%q section should not be empty", sec)
		return false
	}
	return true
}

func (p *Parser) checkSequence(sec string, n *yaml.Node, allowEmpty bool) bool {
	if n.Kind != yaml.SequenceNode {
		p.typeErrorf(n, "%q section must be sequence node but got %s node with %q tag", sec, NodeKindName(n.Kind), n.Tag)
		return false
	}
	return allowEmpty || p.checkNotEmpty(sec, len(n.Content), n)
}

func (p *Parser) CheckString(n *yaml.Node, allowEmpty bool) bool {
	// Do not check n.Tag is !!str because we don't need to check the node is String strictly.
	// In almost all cases, other nodes (like 42) are handled as String with its String representation.
	if n.Kind != yaml.ScalarNode {
		p.typeErrorf(n, "expected scalar node for string value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return false
	}
	if !p.checkRawYAMLTag(n) {
		return false
	}
	if !allowEmpty && n.Value == "" {
		p.error(n, "string should not be empty")
		return false
	}
	return true
}

func (p *Parser) missingExpression(n *yaml.Node, expecting string) {
	p.typeErrorf(n, "expecting a single ${{...}} expression or %s, but found plain text node", expecting)
}

func (p *Parser) parseExpression(n *yaml.Node, expecting string) *String {
	if !IsExprAssigned(n.Value) {
		p.missingExpression(n, expecting)
		return nil
	}
	return newString(n)
}

func (p *Parser) mayParseExpression(n *yaml.Node) *String {
	if n.Tag != YamlTagStr {
		return nil
	}
	if !IsExprAssigned(n.Value) {
		return nil
	}
	return newString(n)
}

func (p *Parser) parseString(n *yaml.Node, allowEmpty bool) *String {
	if !p.CheckString(n, allowEmpty) {
		return &String{"", false, PosAt(n)}
	}
	return newString(n)
}

// parseCondition preserves the distinction between YAML null and the String "null".
func (p *Parser) parseCondition(n *yaml.Node) *String {
	s := p.parseString(n, true)
	if n.Tag == "!!null" {
		s.Value = ""
	}
	if strings.TrimSpace(s.Value) == "" {
		s.Value = "success()"
	}
	return s
}

// parseStaticString folds a sole literal expression before downstream validation.
func (p *Parser) parseStaticString(n *yaml.Node) *String {
	if value := exprlang.LiteralExpressionValue(n.Value); value != nil {
		literal := *n
		literal.Value = *value
		return p.parseString(&literal, false)
	}
	return p.parseString(n, false)
}

func (p *Parser) parseStringSequence(sec string, n *yaml.Node, allowEmpty bool) []*String {
	if ok := p.checkSequence(sec, n, allowEmpty); !ok {
		return nil
	}

	ss := make([]*String, 0, len(n.Content))
	for _, c := range n.Content {
		s := p.parseString(c, false)
		if s != nil {
			ss = append(ss, s)
		}
	}
	return ss
}

func (p *Parser) parseStringOrStringSequence(sec string, n *yaml.Node) []*String {
	switch n.Kind {
	case yaml.ScalarNode:
		return []*String{p.parseString(n, false)}
	default:
		return p.parseStringSequence(sec, n, false)
	}
}

func (p *Parser) parseLiteralBool(n *yaml.Node) *Bool {
	if n.Kind != yaml.ScalarNode || n.Tag != YamlTagBool {
		p.typeErrorf(n, "expected boolean literal but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	return p.parseBool(n)
}

func (p *Parser) parseBool(n *yaml.Node) *Bool {
	if n.Kind != yaml.ScalarNode || (n.Tag != YamlTagBool && n.Tag != YamlTagStr) {
		p.typeErrorf(n, "expected bool value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == YamlTagStr {
		e := p.parseExpression(n, "boolean literal \"true\" or \"false\"")
		return &Bool{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	return &Bool{
		Value: strings.EqualFold(n.Value, "true"),
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseInt(n *yaml.Node) *Int {
	if n.Kind != yaml.ScalarNode || (n.Tag != YamlTagInt && n.Tag != YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for integer value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == YamlTagStr {
		e := p.parseExpression(n, "integer literal")
		if e == nil {
			return nil
		}
		return &Int{
			Expression: e,
			Pos:        PosAt(n),
		}
	}

	i, err := parseYAMLInteger(n.Value)
	if err != nil {
		p.errorf(n, "invalid integer value: %q: %s", n.Value, err.Error())
		return nil
	}

	return &Int{
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
	if !yamlscalar.IsCoreSchemaInt(value) {
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

func (p *Parser) parseFloat(n *yaml.Node) *Float {
	if n.Kind != yaml.ScalarNode || (n.Tag != YamlTagFloat && n.Tag != YamlTagInt && n.Tag != YamlTagStr) {
		p.typeErrorf(n, "expected scalar node for float value but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
		return nil
	}
	if !p.checkRawYAMLTag(n) {
		return nil
	}

	if n.Tag == YamlTagStr {
		e := p.parseExpression(n, "float number literal")
		if e == nil {
			return nil
		}
		return &Float{
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
		p.errorf(n, "invalid float value: %q: %s", n.Value, err.Error())
		return nil
	}
	if math.IsNaN(f) {
		p.errorf(n, "invalid float value: %q: NaN is not a finite number", n.Value)
		return nil
	}

	return &Float{
		Value: f,
		Pos:   PosAt(n),
	}
}

func (p *Parser) parseMapping(where delayedSprintf, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMappingEntries(where, n, allowEmpty, caseSensitive, false)
}

func (p *Parser) parseMappingEntries(where delayedSprintf, n *yaml.Node, allowEmpty, caseSensitive, insertions bool) iter.Seq[workflowMappingEntry] {
	return func(yield func(workflowMappingEntry) bool) {
		if n.Kind == yaml.ScalarNode && n.Tag == YamlTagNull {
			if !allowEmpty {
				p.errorf(n, "%s should not be empty. please remove this section if it's unnecessary", where.String())
			}
			return
		}

		if n.Kind != yaml.MappingNode {
			p.typeErrorf(n, "%s is %s node but mapping node is expected", where.String(), NodeKindName(n.Kind))
			return
		}

		keys := make(map[string]*Pos, len(n.Content)/2)
		empty := true
		for i := 0; i < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			if insertions {
				keyNode = InputResolver.KeyNode(keyNode, newString)
			}
			if !p.CheckString(keyNode, false) {
				p.checkRawYAMLTags(n.Content[i+1])
				empty = false
				continue
			}
			k := newString(n.Content[i])

			if k.Value == "<<" {
				p.errorAt(k.Pos, "GitHub Actions does not support YAML merge key \"<<\"")
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
				insertedKeys := knownInsertedMappingKeys(n.Content[i+1])
				for _, name := range slices.Sorted(maps.Keys(insertedKeys)) {
					inserted := insertedKeys[name]
					if previous, duplicate := keys[name]; duplicate {
						p.errorfAt(inserted.Pos, "key %q is duplicated in %s. previously defined at %s. note that this key is case insensitive", inserted.Value, where.String(), previous.String())
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
				p.errorfAt(k.Pos, "key %q is duplicated in %s. previously defined at %s%s", k.Value, where.String(), pos.String(), note)
				continue
			}

			if !yield(workflowMappingEntry{id, k, n.Content[i+1]}) {
				break
			}

			keys[id] = k.Pos
			empty = false
		}

		if !allowEmpty && empty {
			p.errorf(n, "%s should not be empty. please remove this section if it's unnecessary", where.String())
		}
	}
}

func (p *Parser) parseSectionMapping(section string, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf("%q section", section), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseMappingAt(where string, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf(where, ""), n, allowEmpty, caseSensitive)
}

func (p *Parser) parseScheduleEvent(pos *Pos, n *yaml.Node) *ScheduledEvent {
	if ok := p.checkSequence("schedule", n, false); !ok {
		return nil
	}

	schedules := make([]*ScheduleEntry, 0, len(n.Content))
	for _, c := range n.Content {
		entry := &ScheduleEntry{}
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
		} else if !cronGiven && c.Kind == yaml.MappingNode && len(c.Content) != 0 {
			p.error(c, "\"cron\" is missing in element of \"schedule\" section")
		}
	}

	return &ScheduledEvent{schedules, pos}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatchinputs
func (p *Parser) parseWorkflowDispatchEventInput(name *String, n *yaml.Node) *DispatchInput {
	ret := &DispatchInput{Name: name}

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
				ret.Type = WorkflowDispatchEventInputTypeString
			case "number":
				ret.Type = WorkflowDispatchEventInputTypeNumber
			case "boolean":
				ret.Type = WorkflowDispatchEventInputTypeBoolean
			case "choice":
				ret.Type = WorkflowDispatchEventInputTypeChoice
			case "environment":
				ret.Type = WorkflowDispatchEventInputTypeEnvironment
			default:
				p.errorf(e.val, `input type of workflow_dispatch event must be one of "string", "number", "boolean", "choice", "environment" but got %q`, e.val.Value)
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
func (p *Parser) parseWorkflowDispatchEvent(pos *Pos, n *yaml.Node) *WorkflowDispatchEvent {
	ret := &WorkflowDispatchEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_dispatch", n, true, true) {
		if e.id != "inputs" {
			p.unexpectedKey(e.key, "workflow_dispatch", []string{"inputs"})
			continue
		}

		ret.Inputs = map[string]*DispatchInput{}
		for e := range p.parseSectionMapping("inputs", e.val, true, false) {
			ret.Inputs[e.id] = p.parseWorkflowDispatchEventInput(e.key, e.val)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#repository_dispatch
func (p *Parser) parseRepositoryDispatchEvent(pos *Pos, n *yaml.Node) *RepositoryDispatchEvent {
	ret := &RepositoryDispatchEvent{Pos: pos}

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
func (p *Parser) parseWebhookEventFilter(name *String, n *yaml.Node) *WebhookEventFilter {
	v := p.parseStringOrStringSequence(name.Value, n)
	return &WebhookEventFilter{name, v}
}

func (p *Parser) parseWebhookEvent(name *String, n *yaml.Node) *WebhookEvent {
	ret := &WebhookEvent{Hook: name, Pos: name.Pos}

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
func (p *Parser) parseWorkflowCallEventInput(id string, name *String, n *yaml.Node) *WorkflowCallEventInput {
	ret := &WorkflowCallEventInput{Name: name, ID: id}
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
				ret.Type = WorkflowCallEventInputTypeBoolean
			case "number":
				ret.Type = WorkflowCallEventInputTypeNumber
			case "string":
				ret.Type = WorkflowCallEventInputTypeString
			default:
				p.errorf(e.val, "invalid value %q for input type of workflow_call event. it must be one of \"boolean\", \"number\", or \"string\"", e.val.Value)
			}
		default:
			p.unexpectedKey(e.key, "inputs at workflow_call event", []string{"description", "required", "default", "type"})
		}
	}

	if !typed {
		p.errorfAt(name.Pos, "\"type\" is missing at %q input of workflow_call event", name.Value)
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#example-of-onworkflow_callsecrets
func (p *Parser) parseWorkflowCallEventSecret(name *String, n *yaml.Node) *WorkflowCallEventSecret {
	ret := &WorkflowCallEventSecret{Name: name}

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
func (p *Parser) parseWorkflowCallEventOutput(name *String, n *yaml.Node) *WorkflowCallEventOutput {
	output := &WorkflowCallEventOutput{Name: name}

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
		p.errorfAt(name.Pos, "\"value\" is missing at %q output of workflow_call event", name.Value)
	}

	return output
}

// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow-reuse-events
// - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_call
// - https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
func (p *Parser) parseWorkflowCallEvent(pos *Pos, n *yaml.Node) *WorkflowCallEvent {
	ret := &WorkflowCallEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_call", n, true, true) {
		switch e.id {
		case "inputs":
			ret.Inputs = []*WorkflowCallEventInput{}
			for e := range p.parseSectionMapping("inputs", e.val, true, false) {
				ret.Inputs = append(ret.Inputs, p.parseWorkflowCallEventInput(e.id, e.key, e.val))
			}
		case "secrets":
			ret.Secrets = map[string]*WorkflowCallEventSecret{}
			for e := range p.parseSectionMapping("secrets", e.val, true, false) {
				ret.Secrets[e.id] = p.parseWorkflowCallEventSecret(e.key, e.val)
			}
		case "outputs":
			ret.Outputs = map[string]*WorkflowCallEventOutput{}
			for e := range p.parseSectionMapping("outputs", e.val, true, false) {
				ret.Outputs[e.id] = p.parseWorkflowCallEventOutput(e.key, e.val)
			}
		default:
			p.unexpectedKey(e.key, "workflow_call", []string{"inputs", "secrets", "outputs"})
		}
	}

	return ret
}

func (p *Parser) parseImageVersionEvent(pos *Pos, n *yaml.Node) *ImageVersionEvent {
	ret := &ImageVersionEvent{Pos: pos}

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

func (p *Parser) parseEventWithNoConfig(n *yaml.Node) Event {
	s := p.parseString(n, false)
	switch s.Value {
	case "":
		return nil
	case "schedule":
		p.error(n, "schedule event must be configured with mapping")
		return nil
	case "repository_dispatch":
		return &RepositoryDispatchEvent{Pos: PosAt(n)}
	case "workflow_dispatch":
		return &WorkflowDispatchEvent{Pos: PosAt(n)}
	case "workflow_call":
		return &WorkflowCallEvent{Pos: PosAt(n)}
	case "image_version":
		return &ImageVersionEvent{Pos: PosAt(n)}
	default:
		return &WebhookEvent{Hook: s, Pos: PosAt(n)}
	}
}

func (p *Parser) parseEvents(n *yaml.Node) []Event {
	switch n.Kind {
	case yaml.ScalarNode:
		if e := p.parseEventWithNoConfig(n); e != nil {
			return []Event{e}
		}
		return []Event{}
	case yaml.MappingNode:
		ret := []Event{}
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
	case yaml.SequenceNode:
		l := len(n.Content)
		p.checkNotEmpty("on", l, n)
		ret := make([]Event, 0, l)

		for _, c := range n.Content {
			if e := p.parseEventWithNoConfig(c); e != nil {
				ret = append(ret, e)
			}
		}

		return ret
	default:
		p.typeErrorf(n, "\"on\" section value is expected to be mapping or sequence but found %s node", NodeKindName(n.Kind))
		return nil
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#permissions
func (p *Parser) parsePermissions(pos *Pos, n *yaml.Node) *Permissions {
	ret := &Permissions{Pos: pos}

	if n.Kind == yaml.ScalarNode {
		ret.All = p.parseString(n, false)
	} else {
		// XXX: Is the permission scope case insensitive?
		scopes := map[string]*PermissionScope{}
		for e := range p.parseSectionMapping("permissions", n, true, false) {
			scopes[e.id] = &PermissionScope{
				Name:  e.key,
				Value: p.parseString(e.val, false),
			}
		}
		ret.Scopes = scopes
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#env
func (p *Parser) parseEnv(n *yaml.Node) *Env {
	return p.parseEnvMapping(n, false)
}

func (p *Parser) parseEnvMapping(n *yaml.Node, allowEmpty bool) *Env {
	if n.Kind == yaml.ScalarNode {
		return &Env{
			Expression: p.parseExpression(n, "mapping value for \"env\" section"),
		}
	}

	env := &Env{Vars: map[string]*EnvVar{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "env"), n, allowEmpty, false, true) {
		if exprlang.IsMappingInsertion(e.key.Value) {
			env.Insertions = append(env.Insertions, p.parseEnvMapping(e.val, true))
			continue
		}
		env.Vars[e.id] = &EnvVar{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}

	return env
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#defaults
func (p *Parser) parseDefaults(pos *Pos, n *yaml.Node) *Defaults {
	ret := &Defaults{Pos: pos}

	for e := range p.parseSectionMapping("defaults", n, false, true) {
		if e.id != "run" {
			p.unexpectedKey(e.key, "defaults", []string{"run"})
			continue
		}
		ret.Run = &DefaultsRun{Pos: e.key.Pos}
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
		p.error(n, "\"defaults\" section should have \"run\" section")
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idconcurrency
func (p *Parser) parseConcurrency(pos *Pos, n *yaml.Node) *Concurrency {
	ret := &Concurrency{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == yaml.ScalarNode {
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
				p.errorf(e.val, "invalid value %q for \"queue\" in \"concurrency\" section. valid values are \"single\" and \"max\"", ret.Queue.Value)
			}
		default:
			p.unexpectedKey(e.key, "concurrency", []string{"cancel-in-progress", "group", "queue"})
		}
	}
	if ret.Group == nil {
		p.errorAt(pos, "group name is missing in \"concurrency\" section")
	}
	if ret.Queue != nil && ret.CancelInProgress != nil && ret.Queue.Value == "max" && ret.CancelInProgress.Expression == nil && ret.CancelInProgress.Value {
		p.errorfAt(ret.Queue.Pos, "\"queue: max\" cannot be combined with \"cancel-in-progress: true\" in \"concurrency\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idenvironment
func (p *Parser) parseEnvironment(pos *Pos, n *yaml.Node) *Environment {
	ret := &Environment{Pos: pos}
	if expr := p.mayParseExpression(n); expr != nil {
		ret.Expression = expr
		return ret
	}

	if n.Kind == yaml.ScalarNode {
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
		p.errorAt(pos, "name is missing in \"environment\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idoutputs
func (p *Parser) parseOutputs(n *yaml.Node) map[string]*Output {
	ret := map[string]*Output{}
	for e := range p.parseSectionMapping("outputs", n, false, false) {
		ret[e.id] = &Output{
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
func (p *Parser) checkRawYAMLTag(n *yaml.Node) bool {
	if message := RawYAMLTagError(n); message != "" {
		p.error(n, message)
		return false
	}
	return true
}

// Check tags in values skipped by decoding because their mapping key is invalid.
func (p *Parser) checkRawYAMLTags(n *yaml.Node) {
	p.checkRawYAMLTag(n)
	for _, child := range n.Content {
		p.checkRawYAMLTags(child)
	}
}

func RawYAMLTagError(n *yaml.Node) string {
	if n.Kind != yaml.ScalarNode || n.Style&yaml.TaggedStyle == 0 || n.Tag == YamlTagStr {
		return ""
	}
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return fmt.Sprintf("tag of a quoted or block scalar must be \"!!str\" but got %q", n.Tag)
	}
	var ok bool
	switch n.Tag {
	case YamlTagBool:
		ok = yamlscalar.IsCoreSchemaBool(n.Value)
	case YamlTagNull:
		ok = yamlscalar.IsCoreSchemaNull(n.Value)
	case YamlTagInt:
		ok = yamlscalar.IsCoreSchemaInt(n.Value)
	case YamlTagFloat:
		ok = yamlscalar.IsCoreSchemaFloat(n.Value)
	default:
		return fmt.Sprintf("tag of a YAML scalar must be one of \"!!str\", \"!!bool\", \"!!int\", \"!!float\", \"!!null\" but got %q", n.Tag)
	}
	if !ok {
		return fmt.Sprintf("invalid value %q for %q tag", n.Value, n.Tag)
	}
	return ""
}

func (p *Parser) ParseRawYAMLValue(n *yaml.Node) RawYAMLValue {
	switch n.Kind {
	case yaml.ScalarNode:
		tag := n.Tag
		if !p.checkRawYAMLTag(n) {
			tag = YamlTagStr
		}
		return &RawYAMLString{Value: n.Value, Tag: tag, Position: PosAt(n)}
	case yaml.SequenceNode:
		vs := make([]RawYAMLValue, 0, len(n.Content))
		for _, c := range n.Content {
			if v := p.ParseRawYAMLValue(c); v != nil {
				vs = append(vs, v)
			}
		}
		return &RawYAMLArray{vs, PosAt(n)}
	case yaml.MappingNode:
		m := map[string]RawYAMLValue{}
		for e := range p.parseMappingAt("matrix row value", n, true, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				m[e.id] = v
			}
		}
		return &RawYAMLObject{m, PosAt(n)}
	default:
		p.typeErrorf(n, "unexpected %s node on parsing value in matrix row", NodeKindName(n.Kind))
		return nil
	}
}

// ParseMatrixCombinations parses matrix include/exclude combinations.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-including-additional-values-into-combinations
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-excluding-configurations-from-a-matrix
func (p *Parser) ParseMatrixCombinations(sec string, n *yaml.Node) *MatrixCombinations {
	if n.Kind == yaml.ScalarNode {
		return &MatrixCombinations{
			Expression: p.parseExpression(n, "array of matrix combination"),
		}
	}

	if ok := p.checkSequence(sec, n, false); !ok {
		return nil
	}

	ret := make([]*MatrixCombination, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode {
			if e := p.parseExpression(c, "mapping of matrix combination"); e != nil {
				ret = append(ret, &MatrixCombination{Expression: e})
			}
			continue
		}

		assigns := map[string]*MatrixAssign{}
		for e := range p.parseMapping(sprintf("element in %q section", sec), c, false, false) {
			if v := p.ParseRawYAMLValue(e.val); v != nil {
				assigns[e.id] = &MatrixAssign{e.key, v}
			}
		}
		ret = append(ret, &MatrixCombination{Assigns: assigns})
	}
	return &MatrixCombinations{Combinations: ret}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
func (p *Parser) parseMatrix(pos *Pos, n *yaml.Node) *Matrix {
	return p.ParseMatrixMapping(pos, n, false, false)
}

func (p *Parser) ParseMatrixMapping(pos *Pos, n *yaml.Node, allowEmpty, evaluated bool) *Matrix {
	if n.Kind == yaml.ScalarNode {
		return &Matrix{
			Expression: p.parseExpression(n, "matrix"),
			Pos:        PosAt(n),
		}
	}

	ret := &Matrix{Pos: pos, Rows: make(map[string]*MatrixRow)}

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
			if e.val.Kind == yaml.ScalarNode {
				ret.Rows[e.id] = &MatrixRow{
					Name:       e.key,
					Expression: p.parseExpression(e.val, "array value for matrix variations"),
				}
				continue
			}

			if ok := p.checkSequence("matrix values", e.val, false); !ok {
				continue
			}

			values := make([]RawYAMLValue, 0, len(e.val.Content))
			for _, c := range e.val.Content {
				if v := p.ParseRawYAMLValue(c); v != nil {
					values = append(values, v)
				}
			}

			ret.Rows[e.id] = &MatrixRow{
				Name:   e.key,
				Values: values,
			}
		}
	}

	for _, row := range ret.Rows {
		row.Evaluated = evaluated
	}
	for _, filters := range []*MatrixCombinations{ret.Include, ret.Exclude} {
		if filters != nil {
			for _, combination := range filters.Combinations {
				combination.Evaluated = evaluated
			}
		}
	}
	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idstrategymax-parallel
func (p *Parser) parseMaxParallel(n *yaml.Node) *Int {
	i := p.parseInt(n)
	if i != nil && i.Expression == nil && i.Value <= 0 {
		p.errorf(n, "value at \"max-parallel\" must be greater than zero: %v", i.Value)
	}
	return i
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategy
func (p *Parser) parseStrategy(pos *Pos, n *yaml.Node) *Strategy {
	ret := &Strategy{Pos: pos}
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

func (p *Parser) parseCredentials(pos *Pos, n *yaml.Node) *Credentials {
	ret := &Credentials{Pos: pos}

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
		p.errorAt(pos, "both \"username\" and \"password\" must be specified in \"credentials\" section")
		return nil
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainer
func (p *Parser) parseContainer(sec string, pos *Pos, n *yaml.Node) *Container {
	ret := &Container{Pos: pos}
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

	if n.Kind == yaml.ScalarNode {
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
		p.errorfAt(pos, "\"image\" is missing in %q section", sec)
	}

	return ret
}

func (p *Parser) parseServices(n *yaml.Node) *Services {
	ret := &Services{Pos: PosAt(n)}
	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
	} else {
		// XXX: Is the key case-insensitive?
		ss := map[string]*Service{}
		for e := range p.parseSectionMapping("services", n, false, false) {
			ss[e.id] = &Service{
				Name:      e.key,
				Container: p.parseContainer("services", e.key.Pos, e.val),
			}
		}
		ret.Value = ss
	}
	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
func (p *Parser) parseTimeoutMinutes(n *yaml.Node) *Float {
	f := p.parseFloat(n)
	if f != nil && f.Expression == nil && f.Value <= 0.0 {
		p.errorf(n, "value at \"timeout-minutes\" must be greater than zero: %v", f.Value)
	}
	return f
}

func (p *Parser) parseActionInputMapping(n *yaml.Node, allowEmpty bool, docker *ExecAction) *ActionInputMapping {
	if n.Kind == yaml.ScalarNode {
		expression := p.parseExpression(n, "mapping value for \"with\" section")
		p.parseDockerInputExpression(n, expression, docker)
		return &ActionInputMapping{Expression: expression}
	}
	mapping := &ActionInputMapping{Inputs: map[string]*Input{}}
	for e := range p.parseMappingEntries(sprintf("%q section", "with"), n, allowEmpty, false, true) {
		switch {
		case exprlang.IsMappingInsertion(e.key.Value):
			mapping.Insertions = append(mapping.Insertions, p.parseActionInputMapping(e.val, true, docker))
		case p.parseDockerInput(e, docker):
		default:
			mapping.Inputs[e.id] = &Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
		}
	}
	return mapping
}

func (p *Parser) parseDockerInput(entry workflowMappingEntry, docker *ExecAction) bool {
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

func (p *Parser) parseDockerInputExpression(n *yaml.Node, expression *String, docker *ExecAction) {
	if docker == nil || expression == nil {
		return
	}
	literal, known := WorkflowExpressionLiteral(expression)
	object, isObject := literal.(map[string]any)
	if !known || !isObject {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(object)) {
		key := strings.ToLower(name)
		if key != "entrypoint" && key != "args" {
			continue
		}
		value, scalar := WorkflowScalarString(object[name])
		if !scalar {
			continue
		}
		if key == "entrypoint" && value == "" {
			p.error(n, "string should not be empty")
			continue
		}
		resolved := &String{Value: value, Pos: expression.Pos}
		if key == "entrypoint" {
			docker.Entrypoint, docker.EntrypointEvaluated = resolved, true
		} else {
			docker.Args, docker.ArgsEvaluated = resolved, true
		}
	}
}

func (p *Parser) parseStepExecAction(entries []workflowMappingEntry, isDocker bool) *ExecAction {
	ret := &ExecAction{}

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
			ret.Inputs = map[string]*Input{}
			with := p.parseMappingEntries(sprintf("%q section", "with"), e.val, false, false, true)
			if isDocker {
				for e := range with {
					if exprlang.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, ret))
						continue
					}
					if !p.parseDockerInput(e, ret) {
						ret.Inputs[e.id] = &Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
					}
				}
			} else {
				for e := range with {
					if exprlang.IsMappingInsertion(e.key.Value) {
						ret.InputInsertions = append(ret.InputInsertions, p.parseActionInputMapping(e.val, true, nil))
						continue
					}
					ret.Inputs[e.id] = &Input{Name: e.key, Value: p.parseString(e.val, true), Source: p.ScriptSourceForDeclaration(e.val, e.key.Pos)}
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

func (p *Parser) parseStepExecRun(entries []workflowMappingEntry) *ExecRun {
	ret := &ExecRun{}

	for _, e := range entries {
		switch e.id {
		case "run":
			ret.Run = p.parseString(e.val, false)
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
func (p *Parser) parseStepExecWait(entries []workflowMappingEntry) *ExecWait {
	ret := &ExecWait{}
	waitGiven := false

	for _, e := range entries {
		switch e.id {
		case "wait":
			waitGiven = true
			ret.Names = p.parseStringOrStringSequence("wait", e.val)
		case "wait-all":
			// The schema allows null or a static boolean; false is diagnosed as a no-op.
			if e.val.Tag != YamlTagNull {
				if b := p.parseLiteralBool(e.val); b != nil && !b.Value {
					p.error(e.val, "the value of \"wait-all\" must be true or omitted")
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
		p.errorAt(ret.AllPos, "\"wait\" and \"wait-all\" cannot be specified in the same step")
	}

	return ret
}

// parseStepExecCancel parses a 'cancel' step that cancels running background steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *Parser) parseStepExecCancel(entries []workflowMappingEntry) *ExecCancel {
	ret := &ExecCancel{}

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
func (p *Parser) parseStepExecParallel(entries []workflowMappingEntry) *ExecParallel {
	ret := &ExecParallel{}

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
func (p *Parser) ParseStep(n *yaml.Node) *Step {
	ret := &Step{Pos: PosAt(n)}

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
	var declaredUses *String
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
			if e.val.Kind == yaml.ScalarNode {
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
		p.error(n, "step must run script with \"run\" section or run action with \"uses\" section")
	}

	return ret
}

// ParseSteps parses a sequence of steps.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
func (p *Parser) ParseSteps(n *yaml.Node) []*Step {
	if ok := p.checkSequence("steps", n, false); !ok {
		return nil
	}

	ret := make([]*Step, 0, len(n.Content))

	for _, c := range n.Content {
		if s := p.ParseStep(c); s != nil {
			ret = append(ret, s)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idruns-on
func (p *Parser) parseRunsOn(n *yaml.Node) *Runner {
	if expr := p.mayParseExpression(n); expr != nil {
		return &Runner{Expression: expr}
	}

	if n.Kind == yaml.ScalarNode || n.Kind == yaml.SequenceNode {
		labels := p.parseStringOrStringSequence("runs-on", n)
		return &Runner{Labels: labels}
	}

	r := &Runner{}
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

func (p *Parser) parseSnapshot(pos *Pos, n *yaml.Node) *Snapshot {
	if expr := p.mayParseExpression(n); expr != nil {
		return &Snapshot{Expression: expr}
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return &Snapshot{ImageName: p.parseString(n, false)}
	case yaml.MappingNode:
		ret := &Snapshot{}
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
			p.errorAt(pos, "\"snapshot\" section must have \"image-name\" configuration")
		}
		return ret
	default:
		p.typeErrorf(n, "\"snapshot\" section value must be string or mapping but found %s node", NodeKindName(n.Kind))
		return nil
	}
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
func (p *Parser) parseJob(id *String, n *yaml.Node) *Job {
	ret := &Job{ID: id, Pos: id.Pos}
	call := &WorkflowCall{}

	// JobKeyRequiresSteps identifies keys excluded from reusable workflow calls.
	// https://docs.github.com/en/actions/using-workflows/reusing-workflows#supported-keywords-for-jobs-that-call-a-reusable-workflow
	var stepsOnlyKey *String
	var callOnlyKey *String

	for e := range p.parseMapping(sprintf("%q job", id.Value), n, false, true) {
		k, v := e.key, e.val
		if JobKeyRequiresSteps(e.id) {
			stepsOnlyKey = k
		}
		switch e.id {
		case "name":
			ret.Name = p.parseString(v, true)
		case "needs":
			if v.Kind == yaml.ScalarNode {
				// needs: job1
				ret.Needs = []*String{p.parseString(v, false)}
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
			call.Inputs = map[string]*WorkflowCallInput{}
			for e := range p.parseSectionMapping("with", v, false, false) {
				call.Inputs[e.id] = &WorkflowCallInput{
					Name:  e.key,
					Value: p.parseString(e.val, true),
				}
			}
			callOnlyKey = k
		case "secrets":
			if e.val.Kind == yaml.ScalarNode {
				// `secrets: inherit` special case
				// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_callsecretsinherit
				if e.val.Value == "inherit" {
					call.InheritSecrets = true
				} else {
					p.errorf(e.val, "expected mapping node for secrets or \"inherit\" string node but found %q node", e.val.Value)
				}
			} else {
				call.Secrets = map[string]*WorkflowCallSecret{}
				for e := range p.parseSectionMapping("secrets", v, false, false) {
					call.Secrets[e.id] = &WorkflowCallSecret{
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
				stepsOnlyKey.Pos,
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
			p.errorfAt(id.Pos, "\"steps\" section is missing in job %q", id.Value)
		}
		if ret.RunsOn == nil {
			p.errorfAt(id.Pos, "\"runs-on\" section is missing in job %q", id.Value)
		}
		if callOnlyKey != nil {
			p.errorfAt(
				callOnlyKey.Pos,
				"%q is only available for a reusable workflow call with \"uses\" but \"uses\" is not found in job %q",
				callOnlyKey.Value,
				id.Value,
			)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobs
func (p *Parser) parseJobs(n *yaml.Node) map[string]*Job {
	ret := map[string]*Job{}
	for e := range p.parseSectionMapping("jobs", n, false, false) {
		ret[e.id] = p.parseJob(e.key, e.val)
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions
func (p *Parser) parse(n *yaml.Node) *Workflow {
	p.resolveAliases(n)

	w := &Workflow{}

	if n.Line == 0 {
		n.Line = 1
	}
	if n.Column == 0 {
		n.Column = 1
	}

	if len(n.Content) == 0 {
		p.error(n, "workflow is empty")
		return w
	}
	if root := n.Content[0]; root.Kind == yaml.MappingNode && len(root.Content) != 0 {
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
		p.error(n, "\"on\" section is missing in workflow")
	}
	if w.Jobs == nil {
		p.error(n, "\"jobs\" section is missing in workflow")
	}

	return w
}

// func dumpYAML(n *yaml.Node, level int) {
// 	fmt.Printf("%s%s (%s, %d,%d): %q\n", strings.Repeat(". ", level), NodeKindName(n.Kind), n.Tag, n.Line, n.Column, n.Value)
// 	for _, c := range n.Content {
// 		dumpYAML(c, level+1)
// 	}
// }

func HandleYAMLUnmarshalError(err error) []*SyntaxError {
	es := &yaml.DecodeError{}
	if errors.As(err, &es) {
		errs := make([]*SyntaxError, 0, len(es.Errors))
		for _, e := range es.Errors {
			errs = append(errs, &SyntaxError{
				Message: "could not parse as YAML: " + e.Message,
				Line:    e.Mark.Line,
				Column:  e.Mark.Column,
				Kind:    "syntax-check",
			})
		}
		return errs
	}

	e := &yaml.LoadError{}
	if errors.As(err, &e) {
		return []*SyntaxError{{
			Message: "could not parse as YAML: " + e.Message,
			Kind:    "syntax-check",
			Line:    e.Mark.Line,
			Column:  e.Mark.Column,
		}}
	}

	return []*SyntaxError{{
		Message: "could not parse as YAML: " + err.Error(),
		Kind:    "syntax-check",
	}}
}

// Parse parses given source as byte sequence into workflow syntax tree. It returns all errors
// detected while parsing the input. It means that detecting one error does not stop parsing. Even
// if one or more errors are detected, Parser will try to continue parsing and finding more errors.
func Parse(b []byte) (*Workflow, []*SyntaxError) {
	var n yaml.Node

	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, HandleYAMLUnmarshalError(err)
	}

	// Uncomment for checking YAML tree
	// dumpYAML(&n, 0)

	p := NewSourceParser(b)
	p.IndexScalarSources(&n)
	p.checkFlowMappingNulls(&n, false)
	w := p.parse(&n)
	w.ExpressionSources = p.workflowExpressionSources(&n)

	return w, p.Errors
}

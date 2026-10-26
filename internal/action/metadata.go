// Package action owns action-manifest syntax, source positions, schema validation,
// expression availability and runtime metadata.
package action

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// Input is input metadata in "inputs" section in action.yml metadata file.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#inputs
type Input struct {
	// Name is a name of this input.
	Name string `json:"name"`
	// Required is true when this input is mandatory to run the action.
	Required bool `json:"required"`
	// Deprecated is true when this input is marked as deprecated.
	Deprecated bool `json:"deprecated"`
	// DeprecationMessage is a deprecation message for the deprecated input.
	DeprecationMessage string `json:"deprecation-message"`
}

// Inputs is a map from input ID to its metadata. Keys are in lower case since input
// names are case-insensitive.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#inputs
type Inputs map[string]*Input

// UnmarshalYAML implements codec.Unmarshaler.
func (inputs *Inputs) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	if n.Kind != syntax.MappingNode {
		return expectedMapping("inputs", n)
	}

	type actionInputMetadata struct {
		Required           bool    `yaml:"required"`
		Default            *string `yaml:"default"`
		DeprecationMessage string  `yaml:"deprecationMessage"`
	}

	var err error

	md := make(Inputs, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		v = Node(v)
		if name := Scalar(n.Content[i]); name != nil {
			k = *name
		}

		var m actionInputMetadata
		if err := decodeFields(d, v, "input definition", map[string]any{
			"required": &m.Required, "default": &m.Default, "deprecationmessage": &m.DeprecationMessage,
		}); err != nil {
			return err
		}

		id := strings.ToLower(k)
		if _, ok := md[id]; ok {
			return fmt.Errorf("input %q is duplicated", k)
		}

		// Value of `deprecationMessage: ` is `nil`. So we cannot determine the key exists or not by `v.Decode()` even
		// if we change the type of `DeprecationMessage` to `*string`.
		dep := false
		for i := 0; i < len(v.Content); i += 2 {
			switch strings.ToLower(Key(v.Content[i])) {
			case "deprecationmessage":
				dep = true
			case "description", "required", "default":
				// OK
			default:
				// Do not return this non-fatal error immediately so that this method still can return a parsed metadata
				// even if the error occurs. This is useful for parsing somewhat broken metadata by generate-popular-actions
				// script.
				err = fmt.Errorf("unexpected key %q for definition of input %q", v.Content[i].Value, k)
			}
		}

		md[id] = &Input{k, m.Required && m.Default == nil, dep, strings.TrimSpace(m.DeprecationMessage)}
	}

	*inputs = md
	return err
}

// Output is output metadata in "outputs" section in action.yml metadata file.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#outputs-for-composite-actions
type Output struct {
	Name string `json:"name"`
}

// Outputs is a map from output ID to its metadata. Keys are in lower case since output
// names are case-insensitive.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#outputs-for-composite-actions
type Outputs map[string]*Output

// UnmarshalYAML implements codec.Unmarshaler.
func (inputs *Outputs) UnmarshalYAML(_ *codec.Decoder, n *syntax.Node) error {
	if n.Kind != syntax.MappingNode {
		return expectedMapping("outputs", n)
	}

	md := make(Outputs, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if name := Scalar(n.Content[i]); name != nil {
			k = *name
		}
		id := strings.ToLower(k)
		if _, ok := md[id]; ok {
			return fmt.Errorf("output %q is duplicated", k)
		}
		md[id] = &Output{k}
	}

	*inputs = md
	return nil
}

// ExprString is a string value from action metadata that may contain
// ${{ }} expressions, together with the position of the value node in the metadata file.
type ExprString struct {
	Value  string `json:"value"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// KeyValue is a named, positioned string in action metadata, used for input defaults
// and composite step "with" and "env" mappings.
type KeyValue struct {
	Name  string     `json:"name"`
	Value ExprString `json:"value"`
}

// Step is a step in "steps" section in "runs" section of action.yaml for a
// composite action. The runner only accepts a step which runs a script with "run" and "shell"
// keys, or a step which runs another action with "uses" key.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runssteps
type Step struct {
	// Line is the line where the step starts in action.yaml.
	Line int `json:"line"`
	// Column is the column where the step starts in action.yaml.
	Column int `json:"column"`
	// IsMapping is whether the step is a mapping node. The runner requires every step to be a
	// mapping.
	IsMapping bool `json:"is_mapping"`
	// Keys is the key names of the step mapping in file order.
	Keys []string `json:"keys"`
	// If is the "if" key value when it is a string.
	If *ExprString `json:"if"`
	// Run is the "run" key value when it is a string. It is nil when the key is absent or its
	// value is not a string.
	Run *ExprString `json:"run"`
	// WorkingDirectory is the "working-directory" key value when it is a string.
	WorkingDirectory *ExprString `json:"working_directory"`
	// StepName is the "name" key value when it is a string.
	StepName *ExprString `json:"name"`
	// With holds each "with" input value that is a string.
	With []*KeyValue `json:"with"`
	// Env holds each "env" variable value that is a string.
	Env             []*KeyValue  `json:"env"`
	Shell           *ExprString  `yaml:"-" json:"-"`
	ContinueOnError *ExprString  `yaml:"-" json:"-"`
	WithExpression  *ExprString  `yaml:"-" json:"-"`
	EnvExpression   *ExprString  `yaml:"-" json:"-"`
	ID              *ExprString  `yaml:"-" json:"-"`
	Node            *syntax.Node `yaml:"-" json:"-"`
	// Uses is the value of "uses" key in the step. It is nil when the key is absent or its value
	// is not a string.
	Uses *string `json:"uses"`
}

// UnmarshalYAML implements codec.Unmarshaler.
func (s *Step) UnmarshalYAML(_ *codec.Decoder, n *syntax.Node) error {
	for n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}

	s.Line, s.Column = n.Line, n.Column
	s.Node = n
	if n.Kind != syntax.MappingNode {
		return nil
	}

	s.IsMapping = true
	s.Keys = make([]string, 0, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		key := Key(k)
		s.Keys = append(s.Keys, key)
		switch strings.ToLower(key) {
		case "id":
			s.ID = yamlActionExprString(v)
			if s.ID != nil {
				if literal := exprlang.LiteralExpressionValue(s.ID.Value); literal != nil {
					s.ID.Value = *literal
				}
			}
		case "if":
			s.If = yamlActionExprString(v)
		case "run":
			s.Run = yamlActionExprString(v)
		case "working-directory":
			s.WorkingDirectory = yamlActionExprString(v)
		case "name":
			s.StepName = yamlActionExprString(v)
		case "with":
			s.With = yamlKeyValues(v)
			s.WithExpression = yamlExprString(v)
		case "env":
			s.Env = yamlKeyValues(v)
			s.EnvExpression = yamlExprString(v)
		case "shell":
			s.Shell = yamlActionExprString(v)
		case "continue-on-error":
			s.ContinueOnError = yamlExprString(v)
		case "uses":
			s.Uses = Scalar(v)
		}
	}
	return nil
}

func yamlActionExprString(n *syntax.Node) *ExprString {
	n = Node(n)
	if value := yamlExprString(n); value != nil {
		return value
	}
	if value := Scalar(n); value != nil {
		return &ExprString{Value: *value, Line: n.Line, Column: n.Column}
	}
	return nil
}

func yamlExprString(n *syntax.Node) *ExprString {
	for n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}
	if n.Kind == syntax.ScalarNode && n.Tag == "!!str" {
		return &ExprString{Value: n.Value, Line: n.Line, Column: n.Column}
	}
	return nil
}

func yamlKeyValues(n *syntax.Node) []*KeyValue {
	for n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}
	if n.Kind != syntax.MappingNode {
		return nil
	}
	kvs := make([]*KeyValue, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := yamlExprString(n.Content[i+1])
		if v == nil {
			continue // non-string values (bool/number) cannot hold ${{ }}
		}
		kvs = append(kvs, &KeyValue{Name: Key(n.Content[i]), Value: *v})
	}
	return kvs
}

type actionCompositeSteps []*Step

func (ss *actionCompositeSteps) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	for n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}
	if n.Kind == syntax.ScalarNode && n.Tag == "!!null" {
		*ss = nil
		return nil
	}
	if n.Kind != syntax.SequenceNode {
		return fmt.Errorf(
			"yaml: steps must be sequence node but %s node was found at line:%d, col:%d",
			workflowlang.NodeKindName(n.Kind),
			n.Line,
			n.Column,
		)
	}
	steps := make(actionCompositeSteps, 0, len(n.Content))
	for _, c := range n.Content {
		s := &Step{}
		if err := d.Decode(c, s); err != nil {
			return err
		}
		steps = append(steps, s)
	}
	*ss = steps
	return nil
}

// Runs is "runs" section of action.yaml. It defines how the action is run.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs
type Runs struct {
	// Plugin is the runner-internal plugin action entrypoint.
	Plugin string `yaml:"plugin" json:"plugin,omitempty"`
	// Using is `using` configuration of action.yaml. It defines what runner is used for the action.
	Using string `yaml:"using" json:"using"`
	// Main is `main` configuration of action.yaml for JavaScript action.
	Main string `yaml:"main" json:"main"`
	// Pre is `pre` configuration of action.yaml for JavaScript action.
	Pre string `yaml:"pre" json:"pre"`
	// PreIf is `pre-if` configuration of action.yaml for JavaScript action.
	PreIf string `yaml:"pre-if" json:"pre-if"`
	// Post is `post` configuration of action.yaml for JavaScript action.
	Post string `yaml:"post" json:"post"`
	// PostIf is `post-if` configuration of action.yaml for JavaScript action.
	PostIf string `yaml:"post-if" json:"post-if"`
	// Steps is `steps` configuration of action.yaml for Composite action.
	Steps actionCompositeSteps `yaml:"steps" json:"steps"`
	// Image is `image` of action.yaml for Docker action.
	Image string `yaml:"image" json:"image"`
	// PreEntrypoint is `pre-entrypoint` of action.yaml for Docker action.
	PreEntrypoint string `yaml:"pre-entrypoint" json:"pre-entrypoint"`
	// Entrypoint is `entrypoint` of action.yaml for Docker action.
	Entrypoint string `yaml:"entrypoint" json:"entrypoint"`
	// PostEntrypoint is `post-entrypoint` of action.yaml for Docker action.
	PostEntrypoint string `yaml:"post-entrypoint" json:"post-entrypoint"`
	// Args is `args` of action.yaml for Docker action.
	Args []any `yaml:"args" json:"args"`
	// Env is `env` of action.yaml for Docker action.
	Env map[string]any `yaml:"env" json:"env"`
}

// Branding is "branding" section of action.yaml.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#branding
type Branding struct {
	Icon  string `yaml:"icon"`
	Color string `yaml:"color"`
}

// UnmarshalYAML implements codec.Unmarshaler.
func (r *Runs) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	return decodeFields(d, n, "runs", map[string]any{
		"plugin": &r.Plugin, "using": &r.Using, "main": &r.Main,
		"pre": &r.Pre, "pre-if": &r.PreIf, "post": &r.Post, "post-if": &r.PostIf,
		"steps": &r.Steps, "image": &r.Image,
		"pre-entrypoint": &r.PreEntrypoint, "entrypoint": &r.Entrypoint, "post-entrypoint": &r.PostEntrypoint,
		"args": &r.Args, "env": &r.Env,
	})
}

// UnmarshalYAML implements codec.Unmarshaler.
func (b *Branding) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	return decodeFields(d, n, "branding", map[string]any{"icon": &b.Icon, "color": &b.Color})
}

func decodeFields(d *codec.Decoder, n *syntax.Node, where string, fields map[string]any) error {
	n = Node(n)
	if n.Kind == syntax.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	if n.Kind != syntax.MappingNode {
		return expectedMapping(where, n)
	}
	seen := map[string]bool{}
	var errs []error
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		name := strings.ToLower(Key(key))
		if seen[name] {
			errs = append(errs, &codec.LoadError{Message: fmt.Sprintf("duplicate %s key %q", where, Key(key)), Mark: codec.Mark{Line: key.Line, Column: key.Column}})
			continue
		}
		seen[name] = true
		if target, exists := fields[name]; exists {
			if err := d.Decode(n.Content[i+1], target); err != nil {
				errs = append(errs, err)
				if _, fatal := err.(*codec.LoadError); fatal {
					break
				}
			}
		}
	}
	return errors.Join(errs...)
}

// Key resolves a metadata key using the runner's literal-expression rules.
func Key(n *syntax.Node) string {
	if key := Scalar(n); key != nil {
		return *key
	}
	return n.Value
}

// Fields copies a mapping's keys and normalizes their spelling for struct decoding.
func Fields(node *syntax.Node) *syntax.Node {
	n := Node(node)
	if n.Kind != syntax.MappingNode {
		return n
	}
	normalized := *n
	normalized.Content = append([]*syntax.Node(nil), n.Content...)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := *n.Content[i]
		value := Scalar(&key)
		key.Alias, key.Anchor = nil, ""
		if value != nil {
			key.Kind, key.Style, key.Tag, key.Value = syntax.ScalarNode, 0, "!!str", strings.ToLower(*value)
			if key.Value == "deprecationmessage" {
				key.Value = "deprecationMessage"
			}
		}
		normalized.Content[i] = &key
	}
	return &normalized
}

// Metadata represents structure of action.yaml.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions
type Metadata struct {
	// parsedSource is the owned text captured by Parse, independent of legacy views.
	parsedSource *string
	// SourceMapped records that repository composition supplied a source override.
	SourceMapped bool `yaml:"-" json:"-"`
	// Directory, Filename and Source identify the decoded manifest independently of its caller.
	Directory string `yaml:"-" json:"-"`
	Filename  string `yaml:"-" json:"-"`
	Source    []byte `yaml:"-" json:"-"`
	// Root is the original YAML tree used for schema checks and source mapping.
	Root      *syntax.Node `yaml:"-" json:"-"`
	positions *metadataPositionCache
	// Name is "name" field of action.yaml.
	Name string `yaml:"name" json:"name"`
	// Description is "description" field of action.yaml.
	Description string `yaml:"description" json:"-"`
	// Inputs is "inputs" field of action.yaml.
	Inputs Inputs `yaml:"inputs" json:"inputs"`
	// Outputs is "outputs" field of action.yaml. Key is name of output. Description is omitted
	// since actionlint does not use it.
	Outputs Outputs `yaml:"outputs" json:"outputs"`
	// SkipInputs is flag to specify behavior of inputs check. When it is true, inputs for this
	// action will not be checked.
	SkipInputs bool `yaml:"-" json:"skip_inputs"`
	// SkipOutputs is flag to specify a bit loose typing to outputs object. If it is set to
	// true, the outputs object accepts any properties along with strictly typed props.
	SkipOutputs bool `yaml:"-" json:"skip_outputs"`
	// Runs is "runs" field of action.yaml.
	Runs Runs `yaml:"runs" json:"runs"`
	// Branding is "branding" field of action.yaml.
	Branding Branding `yaml:"branding" json:"-"`
	// InputDefaults holds each string input default and its position in the metadata file.
	InputDefaults []*KeyValue `yaml:"-" json:"-"`
}

// UnmarshalYAML implements codec.Unmarshaler. In addition to the struct tags, it captures the
// value and position of each input's "default" so that ${{ }} expressions used there can be
// checked against the runner's input-default context.
func (md *Metadata) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	var m Metadata
	err := decodeFields(d, n, "action metadata", map[string]any{
		"name": &m.Name, "description": &m.Description,
		"inputs": &m.Inputs, "outputs": &m.Outputs, "runs": &m.Runs, "branding": &m.Branding,
	})
	// The popular-actions generator uses partial metadata after tolerated input errors.
	*md = m
	// The runner turns a single string-literal expression into a literal before
	// dispatching the runtime or resolving entrypoint files.
	for _, value := range []*string{
		&md.Name, &md.Description, &md.Runs.Using, &md.Runs.Plugin,
		&md.Runs.Main, &md.Runs.Pre, &md.Runs.Post,
		&md.Runs.PreIf, &md.Runs.PostIf, &md.Runs.Image,
		&md.Runs.Entrypoint, &md.Runs.PreEntrypoint, &md.Runs.PostEntrypoint,
	} {
		if literal := exprlang.LiteralExpressionValue(*value); literal != nil {
			*value = *literal
		}
	}
	md.Root = n
	md.positions = &metadataPositionCache{}
	md.InputDefaults = collectInputDefaults(n)
	return err
}

// collectInputDefaults walks the raw metadata mapping node and returns the "default" value of
// every input that is a string, together with its position.
func collectInputDefaults(n *syntax.Node) []*KeyValue {
	for n.Kind == syntax.AliasNode && n.Alias != nil && n.Alias.Kind != syntax.AliasNode {
		n = n.Alias
	}
	if n.Kind != syntax.MappingNode {
		return nil
	}

	var inputs *syntax.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		if strings.EqualFold(Key(n.Content[i]), "inputs") {
			inputs = n.Content[i+1]
			break
		}
	}
	if inputs == nil {
		return nil
	}
	for inputs.Kind == syntax.AliasNode && inputs.Alias != nil && inputs.Alias.Kind != syntax.AliasNode {
		inputs = inputs.Alias
	}
	if inputs.Kind != syntax.MappingNode {
		return nil
	}

	var out []*KeyValue
	for i := 0; i+1 < len(inputs.Content); i += 2 {
		name, def := Key(inputs.Content[i]), inputs.Content[i+1]
		for def.Kind == syntax.AliasNode && def.Alias != nil && def.Alias.Kind != syntax.AliasNode {
			def = def.Alias
		}
		if def.Kind != syntax.MappingNode {
			continue
		}
		for j := 0; j+1 < len(def.Content); j += 2 {
			if !strings.EqualFold(Key(def.Content[j]), "default") {
				continue
			}
			if v := yamlExprString(def.Content[j+1]); v != nil {
				out = append(out, &KeyValue{Name: name, Value: *v})
			}
		}
	}
	return out
}

// Dir returns a directory path of the action.
func (md *Metadata) Dir() string {
	return md.Directory
}

// Path returns a file path of the action's metadata file.
func (md *Metadata) Path() string {
	return filepath.Join(md.Directory, md.Filename)
}

func expectedMapping(where string, n *syntax.Node) error {
	return fmt.Errorf("yaml: %s must be mapping node but %s node was found at line:%d, col:%d", where, workflowlang.NodeKindName(n.Kind), n.Line, n.Column)
}

// Parse decodes an action manifest.
// Partially decoded declarations accompany errors for callers that inspect broken metadata.
func Parse(path string, content []byte) (*Metadata, error) {
	metadata := &Metadata{positions: &metadataPositionCache{}}
	owned := bytes.Clone(content)
	err := codec.Unmarshal(owned, metadata)
	text := string(owned)
	metadata.parsedSource = &text
	metadata.Directory, metadata.Filename, metadata.Source = filepath.Dir(path), filepath.Base(path), owned
	return metadata, err
}

// OriginalSource returns the exact text decoded by Parse. Hand-built metadata
// has no source provenance, even when its exported Source field is populated.
func (md *Metadata) OriginalSource() (string, bool) {
	if md == nil || md.parsedSource == nil {
		return "", false
	}
	return *md.parsedSource, true
}

// ScalarSource resolves a decoded value back to its original YAML scalar.
// Source-less metadata and values without a verified mapping return nil.
func (md *Metadata) ScalarSource(value *ExprString) *workflowlang.ScriptSource {
	if md.positions == nil {
		return nil
	}
	return md.positions.source(md, value)
}

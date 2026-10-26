// Package action owns action-manifest syntax, source positions, schema validation,
// expression availability and runtime metadata.
package action

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

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

// Output is output metadata in "outputs" section in action.yml metadata file.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#outputs-for-composite-actions
type Output struct {
	Name string `json:"name"`
}

// Outputs is a map from output ID to its metadata. Keys are in lower case since output
// names are case-insensitive.
// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#outputs-for-composite-actions
type Outputs map[string]*Output

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

type actionCompositeSteps []*Step

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
	root, err := codec.Parse(owned)
	if err == nil && root.Kind != 0 {
		err = Decode(root, metadata)
	}
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

package component

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/analyzer"
	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
	"actionlint.kjanat.dev/rule"
	"github.com/partite-ai/wacogo"
)

const (
	maxScriptInputBytes  = 8 << 20
	maxScriptResultBytes = 8 << 20
	maxScriptDiagnostics = 10000
)

var (
	_ analyzer.ScriptBackend         = (*Plugin)(nil)
	_ analyzer.ScriptConfigValidator = (*Plugin)(nil)
)

// HasScriptEngine reports whether the component implements the snippet role.
func (p *Plugin) HasScriptEngine() bool { return p.scriptRole }

// ScriptDescriptor returns transport-independent snippet engine metadata.
func (p *Plugin) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return p.scriptDescriptor.Clone()
}

// ValidateConfig validates engine-specific JSON through the typed guest API.
func (p *Plugin) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	if !p.scriptRole {
		return errors.New("component does not implement the script-engine role")
	}
	options, err := scriptOptions(config)
	if err != nil {
		return err
	}
	results, err := p.callInterface(ctx, ScriptEngineInterface, "validate-config", wacogo.ValString(options))
	if err != nil {
		return err
	}
	result, ok := results[0].(*wacogo.ValResult)
	if !ok {
		return liftError{"configuration result", results[0]}
	}
	if result.IsOk() {
		return nil
	}
	message, ok := result.Err().(wacogo.ValString)
	if !ok {
		return liftError{"configuration error", result.Err()}
	}
	return fmt.Errorf("script engine %s: %s", p.scriptDescriptor.ID, message)
}

func scriptOptions(config json.RawMessage) (string, error) {
	if len(config) == 0 {
		return "{}", nil
	}
	if len(config) > maxScriptInputBytes {
		return "", errors.New("script engine options exceed 8 MiB")
	}
	var object map[string]json.RawMessage
	if err := jsonv2.Unmarshal(config, &object); err != nil || object == nil {
		return "", errors.New("script engine options must be a JSON object")
	}
	return string(config), nil
}

// Check analyzes a sanitized snippet. Findings and an incomplete-analysis error
// remain independent; malformed findings are rejected before any are published.
func (p *Plugin) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	if !p.scriptRole {
		return nil, errors.New("component does not implement the script-engine role")
	}
	if request.Filename == "" || strings.ContainsRune(request.Filename, 0) ||
		!utf8.ValidString(request.Source) || !utf8.ValidString(request.Filename) {
		return nil, errors.New("script engine needs UTF-8 source and a nonempty virtual filename without NUL")
	}
	if uint64(len(request.Source))+uint64(len(request.Filename))+uint64(len(request.Config)) > maxScriptInputBytes {
		return nil, errors.New("script engine request exceeds 8 MiB")
	}
	options, err := scriptOptions(request.Config)
	if err != nil {
		return nil, err
	}
	input := wacogo.NewValRecord(field("source", wacogo.ValString(request.Source)),
		field("filename", wacogo.ValString(request.Filename)), field("config", wacogo.ValString(options)))
	results, err := p.callInterface(ctx, ScriptEngineInterface, "check", input)
	if err != nil {
		return nil, err
	}
	outcome, err := liftRecord(results[0], "script outcome")
	if err != nil {
		return nil, err
	}
	failure, err := liftScriptOptionalString(outcome.Field("error"), "script error")
	if err != nil {
		return nil, err
	}
	list, err := liftList(outcome.Field("diagnostics"), "script diagnostics")
	if err != nil {
		return nil, err
	}
	if list.Len() > maxScriptDiagnostics {
		return nil, errors.New("script engine returned more than 10000 diagnostics")
	}
	resultBytes := uint64(0)
	if failure != nil {
		resultBytes = uint64(len(*failure))
	}
	if resultBytes > maxScriptResultBytes {
		return nil, errors.New("script engine result exceeds 8 MiB")
	}
	lines := scriptLineWidths(request.Source)
	starts := scriptLineStarts(request.Source)
	diagnostics := make([]analyzer.ScriptDiagnostic, 0, list.Len())
	for i := range list.Len() {
		finding, err := liftScriptDiagnostic(list.Get(i))
		if err != nil {
			return nil, err
		}
		for _, text := range []string{finding.Filename, finding.Code, finding.Name, finding.Message, finding.URL} {
			resultBytes += uint64(len(text))
		}
		if resultBytes > maxScriptResultBytes {
			return nil, errors.New("script engine result exceeds 8 MiB")
		}
		if finding.Filename != request.Filename || finding.Code == "" || finding.Message == "" ||
			!validScriptPosition(lines, finding.Location) || !validScriptPosition(lines, finding.EndLocation) ||
			finding.EndLocation.Row < finding.Location.Row ||
			finding.EndLocation.Row == finding.Location.Row && finding.EndLocation.Column < finding.Location.Column {
			return nil, errors.New("script engine returned an invalid filename, diagnostic or source range")
		}
		// Guests recognize universal newlines; the workflow source mapper uses
		// LF rows. Preserve the exact decoded-source coordinates across lone CRs.
		finding.Location = scriptSourcePosition(starts, finding.Location)
		finding.EndLocation = scriptSourcePosition(starts, finding.EndLocation)
		diagnostics = append(diagnostics, finding)
	}
	if failure != nil {
		return diagnostics, fmt.Errorf("script engine %s: %s", p.scriptDescriptor.ID, *failure)
	}
	return diagnostics, nil
}

// LF, CRLF and lone CR delimit lines; columns count Unicode scalar values.
func scriptLineWidths(source string) []int {
	widths := []int{0}
	for i, character := range source {
		if character == '\r' || character == '\n' {
			if character == '\r' && i+1 < len(source) && source[i+1] == '\n' {
				continue
			}
			widths = append(widths, 0)
		} else {
			widths[len(widths)-1]++
		}
	}
	return widths
}

func scriptLineStarts(source string) []analyzer.ScriptPosition {
	position := analyzer.ScriptPosition{Row: 1, Column: 1}
	starts := []analyzer.ScriptPosition{position}
	for i, character := range source {
		if character == '\n' {
			position.Row++
			position.Column = 1
			starts = append(starts, position)
		} else {
			position.Column++
			if character == '\r' && (i+1 == len(source) || source[i+1] != '\n') {
				starts = append(starts, position)
			}
		}
	}
	return starts
}

func scriptSourcePosition(starts []analyzer.ScriptPosition, position analyzer.ScriptPosition) analyzer.ScriptPosition {
	start := starts[position.Row-1]
	return analyzer.ScriptPosition{Row: start.Row, Column: start.Column + position.Column - 1}
}

func validScriptPosition(lines []int, position analyzer.ScriptPosition) bool {
	return position.Row > 0 && position.Row <= len(lines) && position.Column > 0 &&
		position.Column <= lines[position.Row-1]+1
}

func liftScriptPosition(value wacogo.Val) (analyzer.ScriptPosition, error) {
	r, err := liftRecord(value, "script position")
	if err != nil {
		return analyzer.ScriptPosition{}, err
	}
	row, rowOK := r.Field("row").(wacogo.ValU32)
	column, columnOK := r.Field("column").(wacogo.ValU32)
	if !rowOK || !columnOK || uint64(row) > math.MaxInt || uint64(column) > math.MaxInt {
		return analyzer.ScriptPosition{}, errors.New("invalid script position")
	}
	return analyzer.ScriptPosition{Row: int(row), Column: int(column)}, nil
}

func liftScriptOptionalString(value wacogo.Val, name string) (*string, error) {
	option, ok := value.(*wacogo.ValOption)
	if !ok {
		return nil, liftError{name, value}
	}
	if option.IsNone() {
		return nil, nil
	}
	text, ok := option.Val().(wacogo.ValString)
	if !ok {
		return nil, liftError{name, option.Val()}
	}
	result := string(text)
	return &result, nil
}

func liftScriptDiagnostic(value wacogo.Val) (analyzer.ScriptDiagnostic, error) {
	r, err := liftRecord(value, "script diagnostic")
	if err != nil {
		return analyzer.ScriptDiagnostic{}, err
	}
	var d analyzer.ScriptDiagnostic
	for name, dest := range map[string]*string{"filename": &d.Filename, "code": &d.Code, "name": &d.Name, "message": &d.Message} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
	}
	if d.Severity, err = liftRunnerEnum(r.Field("severity"), script_engine_severity); err != nil {
		return d, err
	}
	if d.Location, err = liftScriptPosition(r.Field("location")); err != nil {
		return d, err
	}
	if d.EndLocation, err = liftScriptPosition(r.Field("end-location")); err != nil {
		return d, err
	}
	url, err := liftScriptOptionalString(r.Field("url"), "diagnostic URL")
	if url != nil {
		d.URL = *url
	}
	return d, err
}

func liftScriptDescriptor(value wacogo.Val) (analyzer.ScriptEngineDescriptor, error) {
	r, err := liftRecord(value, "script descriptor")
	if err != nil {
		return analyzer.ScriptEngineDescriptor{}, err
	}
	var d analyzer.ScriptEngineDescriptor
	for name, dest := range map[string]*string{"id": &d.ID, "version": &d.Version, "name": &d.Name, "engine": &d.Engine, "engine-version": &d.EngineVersion} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
		if strings.TrimSpace(*dest) == "" {
			return d, fmt.Errorf("script descriptor needs %s", name)
		}
	}
	list, err := liftList(r.Field("shells"), "script shells")
	if err != nil {
		return d, err
	}
	seenShells := make(map[string]struct{}, list.Len())
	for i := range list.Len() {
		shell, ok := list.Get(i).(wacogo.ValString)
		_, duplicate := seenShells[string(shell)]
		if !ok || shell == "" || duplicate {
			return d, errors.New("script descriptor contains an empty or duplicate shell")
		}
		// Shell metadata must use canonical runtime selectors:
		// lowercase executable basenames without the .exe suffix.
		name := string(shell)
		if strings.ContainsAny(name, " /\\\t\r\n") || name != strings.TrimSuffix(strings.ToLower(name), ".exe") {
			return d, fmt.Errorf("script descriptor contains noncanonical shell %q", name)
		}
		seenShells[string(shell)] = struct{}{}
		d.Shells = append(d.Shells, string(shell))
	}
	if len(d.Shells) == 0 {
		return d, errors.New("script descriptor needs supported shells")
	}
	d.Rule, err = liftScriptRule(r.Field("rule"))
	if err != nil {
		return d, err
	}
	if d.Rule.Name != d.Engine || d.Rule.Provider != d.ID {
		return d, errors.New("script rule identity disagrees with its engine descriptor")
	}
	return d, nil
}

func liftScriptRule(value wacogo.Val) (rule.Descriptor, error) {
	r, err := liftRecord(value, "script rule descriptor")
	if err != nil {
		return rule.Descriptor{}, err
	}
	d := rule.Descriptor{FixAvailability: "none"}
	for name, dest := range map[string]*string{"name": &d.Name, "description": &d.Description, "category": &d.Category, "subcategory": &d.Subcategory, "maturity": &d.Maturity, "provider": &d.Provider, "explanation": &d.Explanation, "help": &d.Help, "documentation-url": &d.DocumentationURL} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
	}
	if d.Recommended, err = liftBool(r, "recommended"); err != nil {
		return d, err
	}
	schema, err := liftString(r, "options-schema")
	if err != nil {
		return d, err
	}
	if d.OptionsSchema, err = validOptionsSchema(schema); err != nil {
		return d, err
	}
	list, err := liftList(r.Field("examples"), "script examples")
	if err != nil {
		return d, err
	}
	for i := range list.Len() {
		r, err := liftRecord(list.Get(i), "script example")
		if err != nil {
			return d, err
		}
		var example rule.Example
		for name, dest := range map[string]*string{"description": &example.Description, "before": &example.Before, "after": &example.After} {
			if *dest, err = liftString(r, name); err != nil {
				return d, err
			}
		}
		d.Examples = append(d.Examples, example)
	}
	return d, nil
}

func (p *Plugin) verifyScriptMetadata(data []byte) error {
	if data == nil {
		return nil
	}
	var declared struct {
		MetadataVersion int               `json:"metadata_version"`
		ID              string            `json:"id"`
		Name            string            `json:"name"`
		PluginVersion   string            `json:"plugin_version"`
		Transport       string            `json:"transport"`
		Interface       string            `json:"interface"`
		ContractSHA256  string            `json:"contract_sha256"`
		Rules           []rule.Descriptor `json:"rules"`
		ConfigSchema    json.RawMessage   `json:"config_schema"`
		Engine          struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"engine"`
		Capabilities struct {
			ScriptShells []string `json:"script_shells"`
			Fixes        bool     `json:"fixes"`
			Format       bool     `json:"format"`
		} `json:"capabilities"`
	}
	if err := jsonv2.Unmarshal(data, &declared); err != nil {
		return fmt.Errorf("script engine metadata: %w", err)
	}
	d := p.scriptDescriptor
	if declared.MetadataVersion != 1 || declared.Transport != "wasm-component" ||
		declared.Interface != ScriptEngineInterface || declared.ContractSHA256 != scriptEngineContractDigest ||
		declared.ID != d.ID || declared.PluginVersion != d.Version || declared.Name != d.Name ||
		declared.Engine.Name != d.Engine || declared.Engine.Version != d.EngineVersion ||
		!slices.Equal(declared.Capabilities.ScriptShells, d.Shells) ||
		declared.Capabilities.Fixes || declared.Capabilities.Format ||
		len(declared.Rules) != 1 {
		return errors.New("script engine static metadata disagrees with its typed descriptor")
	}
	declaredRule, runtimeRule := declared.Rules[0], d.Rule
	if !sameScriptSchema(declared.ConfigSchema, runtimeRule.OptionsSchema) ||
		!sameScriptSchema(declaredRule.OptionsSchema, runtimeRule.OptionsSchema) {
		return errors.New("script engine static options schema disagrees with its typed descriptor")
	}
	declaredRule.OptionsSchema, runtimeRule.OptionsSchema = nil, nil
	if len(declaredRule.Examples) == 0 {
		declaredRule.Examples = nil
	}
	if !reflect.DeepEqual(declaredRule, runtimeRule) {
		return errors.New("script engine static rule metadata disagrees with its typed descriptor")
	}
	return nil
}

func sameScriptSchema(left, right []byte) bool {
	return len(left) == 0 && len(right) == 0 || pluginartifact.SameMetadata(left, right)
}
